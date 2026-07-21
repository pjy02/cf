package panel

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type Console struct {
	reader *bufio.Reader
	writer io.Writer
	tty    *os.File
}

func OpenConsole() *Console {
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		return &Console{reader: bufio.NewReader(tty), writer: tty, tty: tty}
	}
	return &Console{reader: bufio.NewReader(os.Stdin), writer: os.Stdout}
}

func (c *Console) Close() {
	if c.tty != nil {
		_ = c.tty.Close()
	}
}

func (c *Console) Printf(format string, args ...any) { _, _ = fmt.Fprintf(c.writer, format, args...) }

func (c *Console) ReadLine(prompt string) (string, error) {
	c.Printf("%s", prompt)
	value, err := c.reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (c *Console) ReadSecret(prompt string) (string, error) {
	c.Printf("%s", prompt)
	if c.tty != nil && term.IsTerminal(int(c.tty.Fd())) {
		value, err := term.ReadPassword(int(c.tty.Fd()))
		c.Printf("\n")
		return strings.TrimSpace(string(value)), err
	}
	return c.ReadLine("")
}

func (c *Console) Clear() {
	if c.tty != nil {
		c.Printf("\033[2J\033[H")
	}
}

func (c *Console) Pause() {
	_, _ = c.ReadLine("\n按回车键继续...")
}
