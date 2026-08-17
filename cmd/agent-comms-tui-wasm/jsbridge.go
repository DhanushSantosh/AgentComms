//go:build js && wasm

// This file wires real terminal I/O between xterm.js running in the browser
// and this program's tui.Run call, over syscall/js. There is no real
// terminal on the js/wasm side -- jsInputBuffer/jsOutputWriter are the
// io.Reader/io.Writer tui.Run needs, fed by (and feeding) two JS-callable
// exports and one JS-side callback:
//
//   - JS calls agentCommsTUIWrite(bytes) on every xterm.js onData keystroke;
//     the bytes are appended to jsInputBuffer, which tui.Run reads from.
//   - JS calls agentCommsTUIResize(cols, rows) on load and on every
//     xterm.js onResize; the encoded uv.WindowSizeEvent bytes (see
//     resize.go) are appended to the same jsInputBuffer.
//   - Go calls window.agentCommsTUIOnOutput(text) on every tui.Run write;
//     wasm-bridge.js sets that to `(text) => term.write(text)`.
package main

import (
	"sync"
	"syscall/js"
)

// jsInputBuffer is an io.Reader fed by JS-side bytes (keystrokes from
// xterm.js's onData, and synthetic resize-event bytes from
// agentCommsTUIResize). Read blocks until at least one byte is available --
// there is no EOF; the browser tab's lifetime is the program's lifetime.
type jsInputBuffer struct {
	mu     sync.Mutex
	buf    []byte
	notify chan struct{}
}

func newJSInputBuffer() *jsInputBuffer {
	return &jsInputBuffer{notify: make(chan struct{}, 1)}
}

// write appends p to the buffer and wakes any blocked Read.
func (b *jsInputBuffer) write(p []byte) {
	b.mu.Lock()
	b.buf = append(b.buf, p...)
	b.mu.Unlock()
	select {
	case b.notify <- struct{}{}:
	default:
	}
}

// Read implements io.Reader, blocking until bytes are available.
func (b *jsInputBuffer) Read(p []byte) (int, error) {
	for {
		b.mu.Lock()
		if len(b.buf) > 0 {
			n := copy(p, b.buf)
			b.buf = b.buf[n:]
			b.mu.Unlock()
			return n, nil
		}
		b.mu.Unlock()
		<-b.notify
	}
}

// jsOutputWriter is an io.Writer that forwards every write to xterm.js via
// the window.agentCommsTUIOnOutput callback wasm-bridge.js installs.
type jsOutputWriter struct{}

func (jsOutputWriter) Write(p []byte) (int, error) {
	js.Global().Call("agentCommsTUIOnOutput", string(p))
	return len(p), nil
}

// registerJSBridge installs the two syscall/js exports JS calls into, backed
// by input. It must be called before the goroutine running tui.Run has any
// chance of racing a JS call that arrives before the exports exist -- in
// practice this just means calling it synchronously from main() before
// starting that goroutine, which is what wasm_main.go does.
func registerJSBridge(input *jsInputBuffer) {
	js.Global().Set("agentCommsTUIWrite", js.FuncOf(func(this js.Value, args []js.Value) any {
		data := make([]byte, args[0].Get("length").Int())
		js.CopyBytesToGo(data, args[0])
		input.write(data)
		return nil
	}))
	js.Global().Set("agentCommsTUIResize", js.FuncOf(func(this js.Value, args []js.Value) any {
		cols, rows := args[0].Int(), args[1].Int()
		input.write(encodeWindowSizeEvent(cols, rows))
		return nil
	}))
}
