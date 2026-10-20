package testkit

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// reservePort meminta port kosong ke OS lalu melepasnya (jeda sempit sebelum dipakai ulang).
func reservePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

// serveReplies menjawab setiap baris yang masuk dengan reply (meniru Redis yang sudah siap).
func serveReplies(t *testing.T, addr, reply string) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
					return
				}
				_, _ = conn.Write([]byte(reply))
			}()
		}
	}()
}

func TestWaitRedisReadyReturnsOnceServerAnswersPing(t *testing.T) {
	addr := reservePort(t)
	serveReplies(t, addr, "+PONG\r\n")

	require.NoError(t, waitRedisReady(context.Background(), addr, 5*time.Second))
}

// Skenario nyata yang diperbaiki: container sudah hidup tetapi port host belum menjawab (Docker
// Desktop kewalahan). Menunggu harus bertahan sampai server benar-benar menjawab.
func TestWaitRedisReadyWaitsForServerThatComesUpLate(t *testing.T) {
	addr := reservePort(t)
	go func() {
		time.Sleep(400 * time.Millisecond)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		defer func() { _ = ln.Close() }()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				if _, err := bufio.NewReader(conn).ReadString('\n'); err == nil {
					_, _ = conn.Write([]byte("+PONG\r\n"))
				}
			}()
		}
	}()

	start := time.Now()
	require.NoError(t, waitRedisReady(context.Background(), addr, 5*time.Second))
	require.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond, "harus benar-benar menunggu server naik")
}

func TestWaitRedisReadyTimesOutWhenNothingListens(t *testing.T) {
	addr := reservePort(t)
	err := waitRedisReady(context.Background(), addr, 400*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), addr)
	require.Contains(t, err.Error(), "PING")
}

func TestWaitRedisReadyRejectsNonRedisReply(t *testing.T) {
	addr := reservePort(t)
	serveReplies(t, addr, "HTTP/1.1 400 Bad Request\r\n")

	err := waitRedisReady(context.Background(), addr, 400*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP/1.1 400", "alasan terakhir ikut dilaporkan")
}

func TestWaitRedisReadyStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := waitRedisReady(ctx, reservePort(t), time.Minute)
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second)
}
