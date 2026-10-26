//go:build !windows

package client

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
	"golang.org/x/term"
)

func (c *Client) watchWindowResize(fd int, done chan struct{}) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGWINCH)

	go func() {
		defer signal.Stop(sigChan)
		for {
			select {
			case <-done:
				return
			case <-sigChan:
				w, h, err := term.GetSize(fd)
				if err == nil && w > 0 && h > 0 {
					_ = c.stream.SendJSON(protocol.TypeResize, protocol.ResizePayload{
						Rows: uint16(h),
						Cols: uint16(w),
					})
				}
			}
		}
	}()
}
