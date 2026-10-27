package view

import "github.com/egoist/mygo"

// WindowHost is the Host of a MyGo window.
func WindowHost(win *mygo.Window) Host { return windowHost{win} }

type windowHost struct{ win *mygo.Window }

func (h windowHost) Update(fn func()) { h.win.Update(fn) }

func (h windowHost) ChooseFolder(start string) string {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            h.win,
		DefaultPath:       start,
		ButtonLabel:       "Choose",
		Directory:         true,
		CreateDirectories: true,
	})
	if err != nil || len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func (h windowHost) ChooseFile(title string) string {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:          h.win,
		Title:           title,
		Message:         title,
		ShowHiddenFiles: true,
	})
	if err != nil || len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func (windowHost) ReadClipboard() string      { return mygo.Clipboard.ReadText() }
func (windowHost) WriteClipboard(text string) { mygo.Clipboard.WriteText(text) }
func (windowHost) Reveal(path string)         { mygo.Shell.ShowItemInFolder(path) }
