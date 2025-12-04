package main

import (
	"fmt"
	"time"

	"github.com/konimarti/opc"
	"github.com/konimarti/opc/tests/tools"
	"github.com/sirupsen/logrus"
)

func main() {
	server := "Graybox.Simulator"
	nodes := []string{"localhost"}
	logger := logrus.New().WithField("component", "browser")
	lastRss := uint64(0)
	for i := 0; i < 100000000; i++ {
		tree, err := opc.CreateBrowser(server, nodes, logger)
		if err != nil {
			logger.Fatal("Failed to create browser:", err)
		}
		_ = tree
		rssMb, err := tools.GetRssMb()
		if err != nil {
			panic(err)
		}
		if rssMb > lastRss {
			fmt.Printf("%s current rss: %d MB, increased: %d MB, cycle: %d\n", time.Now(), rssMb, rssMb-lastRss, i)
			lastRss = rssMb
		}
	}
}
