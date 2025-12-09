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
	items := []string{
		"numeric.random.bool",
		"numeric.random.double",
		"numeric.random.float",
		"numeric.random.int16",
		"numeric.random.int32",
		"numeric.random.int64",
		"numeric.random.int8",
		"numeric.random.uint16",
		"numeric.random.uint32",
		"numeric.random.uint64",
		"numeric.random.uint8",
		"numeric.saw.double",
		"numeric.saw.float",
		"numeric.saw.int16",
		"numeric.saw.int32",
		"numeric.saw.int64",
		"numeric.saw.int8",
		"numeric.saw.uint16",
		"numeric.saw.uint32",
		"numeric.saw.uint64",
		"numeric.saw.uint8",
		"numeric.sin.double",
		"numeric.sin.float",
		"numeric.sin.int16",
		"numeric.sin.int32",
		"numeric.sin.int64",
		"numeric.sin.int8",
		"numeric.sin.uint16",
		"numeric.sin.uint32",
		"numeric.sin.uint64",
		"numeric.sin.uint8",
		"numeric.square.bool",
		"numeric.square.double",
		"numeric.square.float",
		"numeric.square.int16",
		"numeric.square.int32",
		"numeric.square.int64",
		"numeric.square.int8",
		"numeric.square.uint16",
		"numeric.square.uint32",
		"numeric.square.uint64",
		"numeric.square.uint8",
		"numeric.triangle.double",
		"numeric.triangle.float",
		"numeric.triangle.int16",
		"numeric.triangle.int32",
		"numeric.triangle.int64",
		"numeric.triangle.int8",
		"numeric.triangle.uint16",
		"numeric.triangle.uint32",
		"numeric.triangle.uint64",
		"numeric.triangle.uint8",
	}
	config := opc.DefaultConnectionConfig()
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	testLogger := logger.WithField("test", "test")
	lastRss := uint64(0)
	conn, err := opc.NewConnection(server, nodes, nil, config, testLogger)
	if err != nil {
		panic(err)
	}
	err = conn.Add(items...)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	for i := 0; i < 100000000; i++ {
		conn.(*opc.OpcConnectionImpl).Fix(true)
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
