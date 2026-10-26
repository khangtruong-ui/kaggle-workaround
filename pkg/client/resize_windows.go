//go:build windows

package client

func (c *Client) watchWindowResize(fd int, done chan struct{}) {
	// On Windows, the terminal subsystem and conhost manage buffer geometry.
}
