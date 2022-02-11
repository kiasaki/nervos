package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"image/color"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"crawshaw.io/sqlite"
	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/key"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/crypto/pbkdf2"
)

type C = layout.Context
type D = layout.Dimensions

var colorWhite = nrgb(0xFFFFFF)
var colorBlack = nrgb(0x000000)
var colorBg = nrgb(0xF9F9F9)
var colorBgDark = nrgb(0xEEEEEE)
var colorPrimary = nrgb(0x7C3AED)

var apiUrl = "https://nervos.kiasaki.com"

var (
	db *sqlite.Conn

	win        *app.Window
	th         *material.Theme
	layoutLock sync.Mutex

	page        string
	pageSubject interface{}

	settings               *Settings
	userHash               string
	authKey                []byte
	dataKey                []byte
	items                  map[int64]*Item
	searchResults          []*Item
	searchIgnoreNextChange bool
	saveChan               chan *Item
	saveTimer              *time.Timer

	authUsernameEditor widget.Editor
	authPasswordEditor widget.Editor
	authButtonClick    widget.Clickable
	searchEditor       widget.Editor
	searchList         widget.List
	searchClicks       []widget.Clickable
	newNoteClick       widget.Clickable
	noteEditor         widget.Editor
)

func main() {
	// init ui state
	win = app.NewWindow(app.Title("nervos"),
		app.Size(dp(1200), dp(768)), app.MinSize(dp(360), dp(360)))
	th = material.NewTheme(gofont.Collection())
	th.Palette.ContrastBg = colorPrimary
	page = "loading"
	authUsernameEditor.Submit = true
	authUsernameEditor.SingleLine = true
	authPasswordEditor.Submit = true
	authPasswordEditor.SingleLine = true
	authPasswordEditor.Mask = '*'
	searchList.Axis = layout.Vertical
	searchEditor.Submit = true
	searchEditor.SingleLine = true
	noteEditor.InputHint = key.HintText

	defer func() {
		if db != nil {
			db.Close()
		}
	}()

	go func() {
		// load initial data
		dataDir, err := app.DataDir()
		check(err)
		check(dbInit(filepath.Join(dataDir, "nervos.db")))
		settings, err = settingsLoad()
		check(err)
		if settings.Username == "" {
			page = "login"
			authUsernameEditor.Focus()
		} else {
			page = "unlock"
			authPasswordEditor.Focus()
		}
		win.Invalidate()

		// sync
		for {
			if win == nil {
				return
			}
			if len(dataKey) == 0 {
				time.Sleep(1 * time.Second)
				continue
			}
			if err := syncChanges(); err != nil {
				log.Println("error syncing:", err)
			}
			time.Sleep(30 * time.Second)
		}
	}()

	go func() {
		saveChan = make(chan *Item, 100)
		saveTimer = time.NewTimer(time.Second)
		lastItem := &Item{}
		save := func(item *Item) {
			if item.ID == 0 {
				return
			}
			if err := itemsSave(dataKey, item); err != nil {
				updateError("saving: " + err.Error())
			}
		}
		for {
			select {
			case i := <-saveChan:
				if lastItem.ID != i.ID {
					save(lastItem)
				}
				lastItem = i
				saveTimer.Reset(time.Second)
			case <-saveTimer.C:
				save(lastItem)
			default:
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	go loop()

	app.Main()
}

func syncChanges() error {
	changes := []Item{}
	for _, i := range items {
		if i.Rev > settings.LastSync {
			changes = append(changes, *i)
		}
	}
	var bs bytes.Buffer
	var err error
	var req *http.Request
	var res *http.Response
	var updates []Item
	err = gob.NewEncoder(&bs).Encode(changes)
	if err != nil {
		return err
	}
	req, err = http.NewRequest("POST", apiUrl, &bs)
	if err != nil {
		return err
	}
	req.Header.Set("userhash", userHash)
	req.Header.Set("passkey", hex.EncodeToString(authKey))
	req.Header.Set("checkpoint", strconv.FormatInt(settings.LastSync, 10))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("non 200 status code: %d", res.StatusCode)
		return err
	}
	err = gob.NewDecoder(res.Body).Decode(&updates)
	if err != nil {
		return err
	}
	for _, remotei := range updates {
		if locali, ok := items[remotei.ID]; ok {
			if remotei.Rev > locali.Rev {
				var ii = remotei
				items[ii.ID] = &ii
				itemsSave(dataKey, &ii)
			}
		} else {
			var ii = remotei
			items[ii.ID] = &ii
			itemsSave(dataKey, &ii)
		}
	}
	settings.LastSync, err = strconv.ParseInt(res.Header.Get("checkpoint"), 10, 64)
	if err != nil {
		return err
	}
	settingsSave(settings)

	log.Printf("synced %d changes, %d updates", len(changes), len(updates))
	return nil
}

func loop() {
	defer func() {
		if err := recover(); err != nil {
			updateError(fmt.Sprintf("panic: %v", err))
			go loop()
		}
	}()

	var ops op.Ops
	for {
		e := <-win.Events()
		switch e := e.(type) {
		case system.DestroyEvent:
			win = nil
			if db != nil {
				db.Close()
			}
			if e.Err != nil {
				log.Println(e.Err)
				os.Exit(1)
			}
			os.Exit(0)
		case system.FrameEvent:
			gtx := layout.NewContext(&ops, e)
			layoutApp(gtx)
			e.Frame(gtx.Ops)
		case key.Event:
			updateKey(e)
		}
	}
}

func layoutApp(g C) {
	// update
	if authButtonClick.Clicked() {
		updateLoginOrUnlock()
	}
	for _, e := range append(authUsernameEditor.Events(), authPasswordEditor.Events()...) {
		if _, ok := e.(widget.SubmitEvent); ok {
			updateLoginOrUnlock()
		}
	}
	if newNoteClick.Clicked() {
		updateNoteNew()
	}
	for _, e := range searchEditor.Events() {
		if _, ok := e.(widget.SubmitEvent); ok {
			if len(searchResults) > 0 {
				updateGoToNote(searchResults[0])
			}
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			if searchIgnoreNextChange {
				searchIgnoreNextChange = false
				break
			}
			updateGoToSearch()
		}
	}
	for i := range searchClicks {
		if searchClicks[i].Clicked() {
			updateGoToNote(searchResults[i])
		}
	}
	for _, e := range noteEditor.Events() {
		if _, ok := e.(widget.ChangeEvent); ok {
			updateNoteSave()
		}
	}

	layoutLock.Lock()
	defer layoutLock.Unlock()

	// layout page
	if page == "loading" {
		layoutLoading(g)
	}
	if page == "error" {
		layoutError(g)
	}
	if page == "login" {
		layoutLogin(g)
	}
	if page == "unlock" {
		layoutUnlock(g)
	}
	if page == "search" {
		layoutSearch(g)
	}
	if page == "note" {
		layoutNote(g)
	}
}

func layoutLoading(g C) {
	layout.Stack{Alignment: layout.Center}.Layout(g,
		layout.Stacked(layoutLabel(th, dp(16), "Loading...")))
}

func layoutError(g C) {
	layout.Stack{Alignment: layout.Center}.Layout(g,
		layout.Stacked(layoutLabel(th, dp(18), "ERROR: "+pageSubject.(string))))
}

func layoutLogin(g C) {
	layout.Stack{Alignment: layout.Center}.Layout(g,
		layout.Stacked(func(g C) D {
			g.Constraints.Max.X = g.Metric.Px(dp(360))
			return layout.UniformInset(dp(8)).Layout(g, func(g C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(g,
					layout.Rigid(layoutHeader(th, "Login")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout),
					layout.Rigid(layoutLabel(th, dp(14), "Username")),
					layout.Rigid(layout.Spacer{Height: dp(4)}.Layout),
					layout.Rigid(layoutInput(th, &authUsernameEditor, "")),
					layout.Rigid(layout.Spacer{Height: dp(8)}.Layout),
					layout.Rigid(layoutLabel(th, dp(14), "Password")),
					layout.Rigid(layout.Spacer{Height: dp(4)}.Layout),
					layout.Rigid(layoutInput(th, &authPasswordEditor, "")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout),
					layout.Rigid(layoutButton(th, &authButtonClick, "Login")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout))
			})
		}))
}

func layoutUnlock(g C) {
	layout.Stack{Alignment: layout.Center}.Layout(g,
		layout.Stacked(func(g C) D {
			g.Constraints.Max.X = g.Metric.Px(dp(360))
			return layout.UniformInset(dp(8)).Layout(g, func(g C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(g,
					layout.Rigid(layoutHeader(th, "Unlock")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout),
					layout.Rigid(layoutLabel(th, dp(14), "Password")),
					layout.Rigid(layout.Spacer{Height: dp(4)}.Layout),
					layout.Rigid(layoutInput(th, &authPasswordEditor, "")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout),
					layout.Rigid(layoutButton(th, &authButtonClick, "Unlock")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout))
			})
		}))
}

func layoutSearch(g C) {
	layout.Flex{
		WeightSum: float32(g.Constraints.Max.Y),
		Axis:      layout.Vertical,
	}.Layout(g,
		layout.Rigid(layoutSearchBar),
		layout.Rigid(layoutPageContent(dp(16), func(g C) D {
			return material.List(th, &searchList).Layout(g, len(searchResults), func(g C, i int) D {
				item := searchResults[i]
				updated := idTime(item.Rev)
				preview := strings.Replace(strings.Trim(item.Data[0:min(80, len(item.Data))], "# "), "\n", " ", -1)
				return material.Clickable(g, &searchClicks[i], func(g C) D {
					g.Constraints.Min.X = g.Constraints.Max.X
					return layout.Inset{Top: dp(8), Bottom: dp(8)}.Layout(g, func(g C) D {
						return layout.Flex{Spacing: layout.SpaceBetween}.Layout(g,
							layout.Rigid(layoutLabel(th, dp(16), preview)),
							layout.Rigid(layoutLabel(th, dp(16), updated.Format("15:04 02 01 2006"))),
						)
					})
				})
			})
		})))
}

func layoutNote(g C) {
	layout.Flex{
		WeightSum: float32(g.Constraints.Max.Y),
		Axis:      layout.Vertical,
	}.Layout(g,
		layout.Rigid(layoutSearchBar),
		layout.Rigid(layoutPageContent(dp(16), func(g C) D {
			g.Constraints.Min.Y = g.Metric.Px(dp(300))
			return material.Editor(th, &noteEditor, "Note").Layout(g)
		})))
}

func layoutSearchBar(g C) D {
	g.Constraints.Max.Y = g.Metric.Px(dp(52))
	return layoutWithBg(colorBg, layoutPageContent(dp(0), func(g C) D {
		return layout.Inset{Right: dp(24)}.Layout(g, func(g C) D {
			return layout.Flex{
				WeightSum: float32(g.Constraints.Max.X),
				Spacing:   layout.SpaceBetween,
			}.Layout(g,
				layout.Rigid(func(g C) D {
					return layout.UniformInset(dp(16)).Layout(g, func(g C) D {
						return material.Editor(th, &searchEditor, "Search").Layout(g)
					})
				}),
				layout.Rigid(func(g C) D {
					g.Constraints.Min.X = g.Metric.Px(dp(52))
					g.Constraints.Max.X = g.Metric.Px(dp(52))
					b := material.Button(th, &newNoteClick, "+")
					b.Color = colorBlack
					b.Background = colorBgDark
					b.CornerRadius = dp(0)
					b.TextSize = dp(28)
					b.Inset.Top = dp(6)
					return b.Layout(g)
				}))
		})
	}))(g)
}

func updateKey(e key.Event) {
	if e.Name == key.NameTab && authUsernameEditor.Focused() {
		authPasswordEditor.Focus()
	}
	if e.Name == key.NameEscape {
		if searchEditor.Focused() {
			searchEditor.SetText("")
		}
		if noteEditor.Focused() {
			x, _ := noteEditor.Selection()
			noteEditor.SetCaret(x, x)
		}
	}
	if e.Name == "L" && e.Modifiers.Contain(key.ModCommand) {
		searchEditor.SetCaret(len(searchEditor.Text()), 0)
		updateGoToSearch()
	}
	if e.Name == "N" && e.Modifiers.Contain(key.ModCommand) {
		updateNoteNew()
	}
}

func updateError(message string) {
	page = "error"
	pageSubject = message
	win.Invalidate()
}

func updateLoginOrUnlock() {
	if page == "login" {
		updateLogin()
	}
	if page == "unlock" {
		updateUnlock()
	}
}

func updateLogin() {
	settings.Username = authUsernameEditor.Text()
	updateUnlock()
}

func updateUnlock() {
	password := []byte(authPasswordEditor.Text())
	authPasswordEditor.SetText("")

	go func() {
		userHashBs := sha256.Sum256([]byte(settings.Username))
		userHash = hex.EncodeToString(userHashBs[:])
		authKey = pbkdf2.Key(password, []byte("auth:"+userHash), 100000, 32, sha256.New)
		dataKey = pbkdf2.Key(password, []byte("data:"+userHash), 100000, 32, sha256.New)

		if len(settings.PasswordCheck) == 0 {
			settings.PasswordCheck = textEncrypt(dataKey, userHash)
			settingsSave(settings)
		} else {
			if bytes.Compare(settings.PasswordCheck, textEncrypt(dataKey, userHash)) != 0 {
				updateError("wrong password")
				go func() {
					time.Sleep(600 * time.Millisecond)
					page = "unlock"
					authPasswordEditor.Focus()
					win.Invalidate()
				}()
				return
			}
		}

		var err error
		allItems, err := itemsLoad(dataKey)
		if err != nil {
			updateError(err.Error())
			return
		}
		items = map[int64]*Item{}
		for _, i := range allItems {
			if i.Data == "" {
				continue
			}
			items[i.ID] = i
		}
		updateGoToSearch()
	}()
}

func updateGoToSearch() {
	layoutLock.Lock()
	defer layoutLock.Unlock()
	page = "search"
	searchEditor.Focus()
	searchResults = []*Item{}
	query := strings.ToLower(searchEditor.Text())
	for _, i := range items {
		if query == "" ||
			strconv.FormatInt(i.ID, 10) == query ||
			strings.Contains(strings.ToLower(i.Data), query) {
			searchResults = append(searchResults, i)
		}
	}
	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Rev > searchResults[j].Rev
	})
	searchClicks = make([]widget.Clickable, len(searchResults))
	win.Invalidate()
}

func updateGoToNote(i *Item) {
	layoutLock.Lock()
	defer layoutLock.Unlock()
	searchIgnoreNextChange = true
	searchEditor.SetText(strconv.FormatInt(i.ID, 10))
	page = "note"
	pageSubject = i
	noteEditor.SetText(i.Data)
	noteEditor.SetCaret(len(i.Data), len(i.Data))
	noteEditor.Focus()
	win.Invalidate()
}

func updateNoteNew() {
	item := &Item{}
	item.ID = id()
	item.Rev = item.ID
	check(itemsSave(dataKey, item))
	items[item.ID] = item
	page = "note"
	pageSubject = item
	noteEditor.SetText("")
	noteEditor.Focus()
	win.Invalidate()
}

func updateNoteSave() {
	if page != "note" {
		return
	}
	item := pageSubject.(*Item)
	text := noteEditor.Text()
	if item.Data == text {
		return
	}
	item.Rev = id()
	item.Data = text
	saveChan <- item
}

func layoutPageContent(padding unit.Value, fn func(C) D) func(C) D {
	return func(g C) D {
		return layout.Flex{Spacing: layout.SpaceAround}.Layout(g,
			layout.Rigid(func(g C) D {
				g.Constraints.Min.X = min(g.Metric.Px(dp(1000)), g.Constraints.Max.X)
				g.Constraints.Max.X = min(g.Metric.Px(dp(1000)), g.Constraints.Max.X)
				return layout.UniformInset(padding).Layout(g, fn)
			}))
	}
}

func layoutHeader(th *material.Theme, title string) func(C) D {
	l := material.Label(th, dp(28), title)
	l.Font.Weight = text.Bold
	return l.Layout
}

func layoutLabel(th *material.Theme, size unit.Value, text string) func(C) D {
	return material.Label(th, size, text).Layout
}

func layoutInput(th *material.Theme, e *widget.Editor, hint string) func(C) D {
	return func(g C) D {
		g.Constraints.Max.Y = g.Metric.Px(dp(36))
		return layoutWithBg(colorBgDark, func(gtx C) D {
			return layout.UniformInset(dp(8)).Layout(gtx, material.Editor(th, e, hint).Layout)
		})(g)
	}
}

func layoutButton(th *material.Theme, c *widget.Clickable, l string) func(C) D {
	return func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		b := material.Button(th, c, l)
		b.CornerRadius = dp(0)
		b.Font.Weight = text.Bold
		return b.Layout(gtx)
	}
}

func layoutWithBg(bg color.NRGBA, w func(C) D) func(C) D {
	return func(gtx C) D {
		return layout.Stack{}.Layout(gtx,
			layout.Stacked(func(gtx C) D {
				r := cToRect(gtx.Constraints)
				defer clip.Rect(r).Push(gtx.Ops).Pop()
				paint.ColorOp{Color: bg}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				return D{Size: r.Max}
			}),
			layout.Expanded(w))
	}
}
