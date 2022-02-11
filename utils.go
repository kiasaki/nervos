package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
)

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
		log.Println("panic:", err)
		panic(err)
	}
}

var idSeq int64 = 0

func id() int64 {
	seq := atomic.AddInt64(&idSeq, 1) - 1
	id := time.Now().UnixNano()/int64(time.Millisecond) - 1262304000000
	id <<= 12
	id |= seq % 4096
	return id
}

func idTime(id int64) time.Time {
	unix := (id >> 12) + 1262304000000
	return time.Unix(unix/1000, unix%1000*int64(time.Millisecond))
}

func uuid() string {
	u := [16]byte{}
	_, err := rand.Read(u[:16])
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", u)
}

func mustCipher(key []byte) cipher.Block {
	c, err := aes.NewCipher(key)
	check(err)
	return c
}

func textEncrypt(key []byte, text string) []byte {
	c := mustCipher(key)
	data := []byte(text)
	for len(data)%c.BlockSize() != 0 || len(data) == 0 {
		data = append(data, 0)
	}
	c.Encrypt(data, data)
	return data
}

func textDecrypt(key []byte, data []byte) string {
	mustCipher(key).Decrypt(data, data)
	return strings.Trim(string(data), string(byte(0)))
}
