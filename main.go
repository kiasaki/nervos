package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/component"
	"golang.org/x/crypto/pbkdf2"
)

type C = layout.Context
type D = layout.Dimensions

var colorWhite = nrgb(0xFFFFFF)
var colorBg = nrgb(0xF9F9F9)
var colorBgDark = nrgb(0xEEEEEE)

var db *sqlite.Conn
var windowsWg sync.WaitGroup

func main() {
	dbInit()
	NewApp().Start()
	go func() {
		windowsWg.Wait()
		if db != nil {
			db.Close()
		}
		os.Exit(0)
	}()
	app.Main()
}

type App struct {
	mx  sync.Mutex
	win *app.Window
	th  *material.Theme

	username  string
	userHash  string
	authKey   []byte
	dataKey   []byte
	notes     []*Item
	passwords []*Item

	sidebar         widget.List
	sidebarClicks   []widget.Clickable
	sidebarSplit    component.Resize
	replyEditor     widget.Editor
	newAccountClick widget.Clickable

	loginUsernameEditor widget.Editor
	loginPasswordEditor widget.Editor
	loginButtonClick    widget.Clickable
}

func NewApp() *App {
	a := &App{}
	a.win = app.NewWindow(
		app.Title("nervos"),
		app.Size(dp(1200), dp(768)),
	)
	a.th = material.NewTheme(gofont.Collection())
	a.th.Palette.ContrastBg = nrgb(0x7C3AED)

	a.sidebar.Axis = layout.Vertical
	a.sidebarClicks = make([]widget.Clickable, 1)
	a.sidebarSplit.Ratio = 0.2

	a.loginUsernameEditor.SingleLine = true
	a.loginPasswordEditor.SingleLine = true
	a.loginPasswordEditor.Mask = '*'
	return a
}

func (a *App) Start() {
	go func() {
		var err error
		a.notes, err = itemsLoad(a.dataKey, "notes", a.userHash)
		check(err)
		for {
			if a.win == nil {
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()
	go func() {
		windowsWg.Add(1)
		defer windowsWg.Done()
		err := a.runLoop()
		if err != nil {
			log.Fatalln(err)
		}
	}()
}

func (a *App) runLoop() error {
	var ops op.Ops
	for {
		e := <-a.win.Events()
		switch e := e.(type) {
		case system.DestroyEvent:
			return e.Err
		case system.FrameEvent:
			a.Update()
			gtx := layout.NewContext(&ops, e)
			a.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) Update() {
	if a.loginButtonClick.Clicked() {
		a.username = a.loginUsernameEditor.Text()
		userHash := sha256.Sum256([]byte(a.username))
		a.userHash = hex.EncodeToString(userHash[:])
		a.authKey = pbkdf2.Key(
			[]byte(a.loginPasswordEditor.Text()),
			[]byte("auth:"+a.userHash),
			100000, 32, sha256.New)
		a.dataKey = pbkdf2.Key(
			[]byte(a.loginPasswordEditor.Text()),
			[]byte("data:"+a.userHash),
			100000, 32, sha256.New)
	}
	if a.newAccountClick.Clicked() {
		log.Println("cliced")
	}
	for i := range a.sidebarClicks {
		if a.sidebarClicks[i].Clicked() {
		}
	}
}

func (a *App) Layout(gtx C) {
	layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Stacked(func(gtx C) D {
			gtx.Constraints.Max.X = 360
			return layout.UniformInset(dp(8)).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(layoutHeader(a.th, "Login")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout),
					layout.Rigid(material.Label(a.th, dp(14), "Username").Layout),
					layout.Rigid(layout.Spacer{Height: dp(4)}.Layout),
					layout.Rigid(layoutInput(a.th, &a.loginUsernameEditor, "")),
					layout.Rigid(layout.Spacer{Height: dp(8)}.Layout),
					layout.Rigid(material.Label(a.th, dp(14), "Password").Layout),
					layout.Rigid(layout.Spacer{Height: dp(4)}.Layout),
					layout.Rigid(layoutInput(a.th, &a.loginPasswordEditor, "")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout),
					layout.Rigid(layoutButton(a.th, &a.loginButtonClick, "Login")),
					layout.Rigid(layout.Spacer{Height: dp(16)}.Layout))
			})
		}))

	/*
			a.sidebarSplit.Layout(gtx, layoutWithBg(colorBg, func(gtx C) D {
				return layout.Flex{
		      WeightSum: float32(gtx.Constraints.Max.Y),
		      Axis: layout.Vertical,
		      Spacing: layout.SpaceBetween
		    }.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.UniformInset(dp(8)).Layout(gtx, func(gtx C) D {
							listStyle := material.List(a.th, &a.sidebar)
							return listStyle.Layout(gtx, 1, func(gtx C, i int) D {
								labelStyle := material.Label(a.th, dp(16), "...")
								//labelStyle.Font.Variant = "Mono"
								return material.Clickable(gtx, &a.sidebarClicks[i], labelStyle.Layout)
							})
						})
					}),
					layout.Flexed(40, func(gtx C) D {
						return material.Clickable(gtx, &a.newAccountClick, layoutWithBg(colorBgDark, func(gtx C) D {
							labelStyle := material.Label(a.th, dp(16), "New Account")
							labelStyle.Alignment = text.Middle
							return layout.UniformInset(dp(8)).Layout(gtx, labelStyle.Layout)
						}))
					}))
			}), func(gtx C) D {
				return D{Size: gtx.Constraints.Max}
			}, func(gtx C) D {
				r := rLeft(cToRect(gtx.Constraints), 6)
				return D{Size: r.Max}
			})
	*/
}

func layoutHeader(th *material.Theme, title string) func(C) D {
	l := material.Label(th, dp(28), title)
	l.Font.Weight = text.Bold
	return l.Layout
}

func layoutInput(th *material.Theme, e *widget.Editor, hint string) func(C) D {
	return func(gtx C) D {
		gtx.Constraints.Max.Y = 36
		return layoutWithBg(colorBgDark, func(gtx C) D {
			return layout.UniformInset(dp(8)).Layout(gtx, material.Editor(th, e, hint).Layout)
		})(gtx)
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

func dbInit() {
	dataDir, err := app.DataDir()
	check(err)
	db, err = sqlite.OpenConn(filepath.Join(dataDir, "nervos.db"), 0)
	check(err)
	check(sqlitex.Exec(db, "create table if not exists items (id int primary key, parent int, type text, created int, updated int, metadata blob, data blob);", nil))
	check(sqlitex.Exec(db, "create index if not exists items_parent on items (parent);", nil))
}

type Item struct {
	ID       string
	Parent   string
	Type     string
	Created  time.Time
	Updated  time.Time
	Metadata map[string]interface{}
	Data     map[string]interface{}
}

func itemsLoad(key []byte, typ, parent string) ([]*Item, error) {
	items := []*Item{}
	fn := func(stmt *sqlite.Stmt) error {
		i := &Item{}
		i.ID = stmt.ColumnText(0)
		i.Parent = stmt.ColumnText(1)
		i.Type = stmt.ColumnText(2)
		i.Created = time.Unix(stmt.ColumnInt64(3), 0)
		i.Updated = time.Unix(stmt.ColumnInt64(4), 0)
		metadata := []byte{}
		stmt.ColumnBytes(5, metadata)
		if err := json.Unmarshal([]byte(metadata), &i.Metadata); err != nil {
			return err
		}
		data := []byte{}
		stmt.ColumnBytes(6, data)
		mustCipher(key).Decrypt(data, data)
		return json.Unmarshal([]byte(data), &i.Data)
	}
	err := sqlitex.Exec(db, "select id, parent, type, created, updated, metadata, data from items where type = ? and parent = ?;", fn, typ, parent)
	return items, err
}

func itemSave(key []byte, i *Item) error {
	metadata, err := json.Marshal(i.Metadata)
	if err != nil {
		return err
	}
	data, err := json.Marshal(i.Data)
	if err != nil {
		return err
	}
	mustCipher(key).Encrypt(data, data)
	return sqlitex.Exec(db, "insert into items (id, parent, type, created, updated, metadata, data) values (?, ?, ?, ?, ?, ?, ?) on conflict (id) do update set created = excluded.created, updated = excluded.updated, metadata = excluded.metadata, data = excluded.data;",
		nil, i.ID, i.Parent, i.Type, i.Created, i.Updated, string(metadata), string(data))
}

func mustCipher(key []byte) cipher.Block {
	c, err := aes.NewCipher(key)
	check(err)
	return c
}

func dp(v float32) unit.Value {
	return unit.Dp(v)
}

func nrgb(c uint32) color.NRGBA {
	return nargb(0xff000000 | c)
}

func nargb(c uint32) color.NRGBA {
	return color.NRGBA{A: uint8(c >> 24), R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c)}
}

func cToRect(c layout.Constraints) image.Rectangle {
	return image.Rectangle{Min: c.Min, Max: c.Max}
}

func rLeft(r image.Rectangle, n int) image.Rectangle {
	return image.Rect(r.Min.X, r.Min.Y, r.Min.X+n, r.Max.Y)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func uuid() string {
	u := [16]byte{}
	_, err := rand.Read(u[:16])
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", u)
}
