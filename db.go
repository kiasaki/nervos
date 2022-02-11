package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

type Settings struct {
	Version       int64
	Username      string
	PasswordCheck []byte
	LastSync      int64
}

type Item struct {
	ID   int64
	Rev  int64
	Data string
}

func dbInit(path string) error {
	log.Println("database: opening:", path)
	var err error
	err = os.MkdirAll(filepath.Dir(path), os.ModePerm)
	if err != nil {
		return err
	}
	db, err = sql.Open("sqlite3", path)
	db.SetMaxOpenConns(1)
	log.Println("database: opened", err)
	if err != nil {
		return err
	}
	_, err = db.Exec("create table if not exists settings (version int, username text, password_check blob, last_sync int);")
	if err != nil {
		return err
	}
	_, err = db.Exec("create table if not exists items (id int primary key, rev int, data blob);")
	return err
}

func settingsLoad() (*Settings, error) {
	settings := &Settings{}
	row := db.QueryRow("select version, username, password_check, last_sync from settings limit 1;")
	err := row.Scan(&settings.Version, &settings.Username, &settings.PasswordCheck, &settings.LastSync)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if settings.Version == 0 {
		settings.Version = 1
		sql := "insert into settings (version, username, password_check, last_sync) values (?, ?, ?, ?);"
		if _, err := db.Exec(sql, 1, "", []byte{}, 0); err != nil {
			return nil, err
		}
	}
	return settings, nil
}

func settingsSave(s *Settings) error {
	_, err := db.Exec("update settings set version = ?, username = ?, password_check = ?, last_sync = ?;",
		s.Version, s.Username, s.PasswordCheck, s.LastSync)
	return err
}

func itemsLoad(key []byte) ([]*Item, error) {
	items := []*Item{}
	rows, err := db.Query("select id, rev, data from items;")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		item := &Item{}
		data := []byte{}
		err = rows.Scan(&item.ID, &item.Rev, &data)
		item.Data = textDecrypt(key, data)
		items = append(items, item)
	}
	return items, rows.Err()
}

func itemsSave(key []byte, i *Item) error {
	_, err := db.Exec("insert into items (id, rev, data) values (?, ?, ?) on conflict (id) do update set data = excluded.data;",
		i.ID, i.Rev, textEncrypt(key, i.Data))
	return err
}
