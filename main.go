package main

import (
	_ "embed"

	"github.com/halpworld/halpradio/pkg/app"
)

//go:embed stations.yaml
var embeddedStations []byte

func main() {
	app.Run(embeddedStations)
}
