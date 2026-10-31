// Command pixelart writes the pictures of the package pixelart to view/html: the wood tile of the rail, the ground tile of
// the page, the sparks of the embers ground, the sprite sheet and the icon sheet. Run it from extensions/web after a change
// to a drawing: go run ./tools/pixelart. A test of the package view compares each file with its drawing.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"

	"lifelog-web/pixelart"
)

func main() {
	for _, f := range []struct {
		name string
		img  image.Image
	}{
		{"wood.png", pixelart.Wood()},
		{"ground.png", pixelart.Ground()},
		{"embers.png", pixelart.Embers()},
		{"sprites.png", pixelart.Sprites()},
		{"icons.png", pixelart.Icons()},
	} {
		path := "view/html/" + f.name
		var buf bytes.Buffer
		if err := png.Encode(&buf, f.img); err != nil {
			fmt.Fprintln(os.Stderr, "pixelart:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "pixelart:", err)
			os.Exit(1)
		}
		fmt.Printf("%s: %d bytes\n", path, buf.Len())
	}
}
