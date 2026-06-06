//go:build linux

package nemotron

import (
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// fakeServer accepts ONE connection, reads a full request frame, replies with `reply`.
func fakeServer(t *testing.T, reply string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "nemo.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var hlen uint32
		if binary.Read(conn, binary.LittleEndian, &hlen) != nil {
			return
		}
		hbuf := make([]byte, hlen)
		io.ReadFull(conn, hbuf)
		var plen uint32
		if binary.Read(conn, binary.LittleEndian, &plen) != nil {
			return
		}
		pbuf := make([]byte, plen)
		io.ReadFull(conn, pbuf)
		body := []byte(reply)
		binary.Write(conn, binary.LittleEndian, uint32(len(body)))
		conn.Write(body)
	}()
	t.Cleanup(func() { ln.Close() })
	return sock
}

func TestTranscribeReturnsBestText(t *testing.T) {
	sock := fakeServer(t, `{"ok":true,"candidates":[{"text":"привет мир","score":-0.1}]}`)
	c := New(Config{SocketPath: sock, Lang: "ru-RU", Timeout: 2 * time.Second})
	got, err := c.Transcribe([]float32{0.0, 0.1, -0.1})
	if err != nil {
		t.Fatal(err)
	}
	if got != "привет мир" {
		t.Fatalf("got %q", got)
	}
}

func TestRecognizeMultipleVariants(t *testing.T) {
	sock := fakeServer(t, `{"ok":true,"candidates":[{"text":"a","score":-1},{"text":"b","score":-2}]}`)
	c := New(Config{SocketPath: sock, Timeout: 2 * time.Second})
	cands, err := c.Recognize([]float32{0.1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || cands[0].Text != "a" || cands[1].Text != "b" {
		t.Fatalf("got %+v", cands)
	}
}

func TestErrorResponse(t *testing.T) {
	sock := fakeServer(t, `{"ok":false,"error":"boom"}`)
	c := New(Config{SocketPath: sock, Timeout: 2 * time.Second})
	if _, err := c.Transcribe([]float32{0.1}); err == nil {
		t.Fatal("expected error")
	}
}
