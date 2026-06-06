//go:build linux

// Package nemotron is the Go client for the Python Nemotron recognition sidecar.
package nemotron

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"time"
)

// Config configures the sidecar client.
type Config struct {
	SocketPath string
	Lang       string  // e.g. "ru-RU"
	Alpha      float64 // biasing weight passed to the sidecar header
	Timeout    time.Duration
}

// Candidate is one recognition hypothesis from the sidecar.
type Candidate struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

type response struct {
	OK         bool        `json:"ok"`
	Error      string      `json:"error"`
	Candidates []Candidate `json:"candidates"`
}

// Client talks to the Nemotron sidecar over a Unix domain socket.
type Client struct{ cfg Config }

// New returns a client. A zero Timeout defaults to 30s.
func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{cfg: cfg}
}

func (c *Client) dial() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.cfg.SocketPath, c.cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("nemotron dial %s: %w", c.cfg.SocketPath, err)
	}
	_ = conn.SetDeadline(time.Now().Add(c.cfg.Timeout))
	return conn, nil
}

func writeFrame(conn net.Conn, header []byte, pcm []float32) error {
	if err := binary.Write(conn, binary.LittleEndian, uint32(len(header))); err != nil {
		return err
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	pcmBytes := make([]byte, 4*len(pcm))
	for i, v := range pcm {
		binary.LittleEndian.PutUint32(pcmBytes[i*4:], math.Float32bits(v))
	}
	if err := binary.Write(conn, binary.LittleEndian, uint32(len(pcmBytes))); err != nil {
		return err
	}
	_, err := conn.Write(pcmBytes)
	return err
}

func readResponse(conn net.Conn) (response, error) {
	var blen uint32
	if err := binary.Read(conn, binary.LittleEndian, &blen); err != nil {
		return response{}, fmt.Errorf("nemotron read len: %w", err)
	}
	body := make([]byte, blen)
	if _, err := io.ReadFull(conn, body); err != nil {
		return response{}, fmt.Errorf("nemotron read body: %w", err)
	}
	var resp response
	if err := json.Unmarshal(body, &resp); err != nil {
		return response{}, fmt.Errorf("nemotron decode: %w", err)
	}
	if !resp.OK {
		return response{}, fmt.Errorf("nemotron: %s", resp.Error)
	}
	return resp, nil
}

// Recognize sends PCM and returns all variant candidates.
func (c *Client) Recognize(pcm []float32, variants int) ([]Candidate, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	header, _ := json.Marshal(map[string]any{
		"lang": c.cfg.Lang, "variants": variants, "alpha": c.cfg.Alpha,
	})
	if err := writeFrame(conn, header, pcm); err != nil {
		return nil, fmt.Errorf("nemotron write: %w", err)
	}
	resp, err := readResponse(conn)
	if err != nil {
		return nil, err
	}
	return resp.Candidates, nil
}

// Ping checks the sidecar is up and the model is loaded (variants:0, no model run).
func (c *Client) Ping() error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	header, _ := json.Marshal(map[string]any{"lang": c.cfg.Lang, "variants": 0})
	if err := writeFrame(conn, header, nil); err != nil {
		return err
	}
	_, err = readResponse(conn)
	return err
}

// Transcribe satisfies app.Transcriber: single variant, best (first) text.
func (c *Client) Transcribe(pcm []float32) (string, error) {
	cands, err := c.Recognize(pcm, 1)
	if err != nil {
		return "", err
	}
	if len(cands) == 0 {
		return "", nil
	}
	return cands[0].Text, nil
}
