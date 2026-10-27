// YTGrab is a desktop front end for yt-dlp. main only wires the packages
// together; see docs/design.md.
package main

import (
	"log"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"

	"github.com/aatng-gh/ytgrab/internal/cookies"
	"github.com/aatng-gh/ytgrab/internal/settings"
	"github.com/aatng-gh/ytgrab/internal/tools"
	"github.com/aatng-gh/ytgrab/internal/view"
)

func main() {
	resources, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		log.Fatal(err)
	}
	userData, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	dirs := tools.Dirs{
		Bundled: filepath.Join(resources, "bin"),
		Managed: filepath.Join(userData, "bin"),
	}

	// A crash or force quit skips OnWillQuit and can leave a cookie jar.
	cookies.SweepStale()

	app := view.New(settings.Open(userData), dirs)

	mygo.Use(native.Plugin)
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { app.Close() })

	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "YTGrab", Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleAbout},
				updater.MenuItem(),
				{Label: "Update yt-dlp", Click: func(*mygo.MenuItem, *mygo.Window) { app.UpdateYtDLP() }},
				mygo.Separator(),
				{Role: mygo.RoleHide},
				{Role: mygo.RoleHideOthers},
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			}},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))

		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "YTGrab",
			Width:     520,
			Height:    640,
			MinWidth:  500,
			MinHeight: 520,
			StateKey:  "main",
			Content:   ui.View(app.View),
		})
		app.Attach(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
