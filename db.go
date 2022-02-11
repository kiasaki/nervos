package main

import (
	"io/ioutil"

	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
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
	var err error
	db, err = sqlite.OpenConn(path, 0)
	if err != nil {
		return err
	}
	err = sqlitex.Exec(db, "create table if not exists settings (version int, username text, password_check blob, last_sync int);", nil)
	if err != nil {
		return err
	}
	return sqlitex.Exec(db, "create table if not exists items (id int primary key, rev int, data blob);", nil)
}

func settingsLoad() (*Settings, error) {
	settings := &Settings{}
	fn := func(stmt *sqlite.Stmt) error {
		settings.Version = stmt.ColumnInt64(0)
		settings.Username = stmt.ColumnText(1)
		settings.PasswordCheck, _ = ioutil.ReadAll(stmt.ColumnReader(2))
		settings.LastSync = stmt.ColumnInt64(3)
		return nil
	}
	err := sqlitex.Exec(db, "select version, username, password_check, last_sync from settings limit 1;", fn)
	if err == nil && settings.Version == 0 {
		settings.Version = 1
		sql := "insert into settings (version, username, password_check, last_sync) values (?, ?, ?, ?);"
		if err := sqlitex.Exec(db, sql, nil, 1, "", []byte{}, 0); err != nil {
			return settings, err
		}
	}
	return settings, err
}

func settingsSave(s *Settings) error {
	return sqlitex.Exec(db, "update settings set version = ?, username = ?, password_check = ?, last_sync = ?;",
		nil, s.Version, s.Username, s.PasswordCheck, s.LastSync)
}

func itemsLoad(key []byte) ([]*Item, error) {
	items := []*Item{}
	fn := func(stmt *sqlite.Stmt) error {
		i := &Item{}
		i.ID = stmt.ColumnInt64(0)
		i.Rev = stmt.ColumnInt64(1)
		data, err := ioutil.ReadAll(stmt.ColumnReader(2))
		if err != nil {
			return err
		}
		i.Data = textDecrypt(key, data)
		items = append(items, i)
		return nil
	}
	err := sqlitex.Exec(db, "select id, rev, data from items;", fn)
	return items, err
}

func itemsSave(key []byte, i *Item) error {
	return sqlitex.Exec(db, "insert into items (id, rev, data) values (?, ?, ?) on conflict (id) do update set data = excluded.data;",
		nil, i.ID, i.Rev, textEncrypt(key, i.Data))
}
