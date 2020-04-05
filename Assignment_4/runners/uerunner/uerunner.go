// Partner 1: 2020-10-0148
// Partner 2: 2020-10-0287

package main

import (
	"flag"
	"log"
	"net/rpc"
	"strconv"
	"tinyepc/rpcs"
)

var (
	lbPort = flag.Int("port", 8000, "Port of the LoadBalancer")
)

func init() {
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
}

func main() {
	flag.Parse()

	lbAddr := ":" + strconv.Itoa(*lbPort)
	conn, err := rpc.DialHTTP("tcp", lbAddr)
	if err != nil {
		return
	}
	var ra *rpcs.UERequestArgs = new(rpcs.UERequestArgs)
	var rr *rpcs.UERequestReply = new(rpcs.UERequestReply)
	ra.UserID = 42069
	ra.UEOperation = rpcs.Call
	conn.Call("LoadBalancer.RecvUERequest", ra, rr)

	// TODO: Implement this!

	select {}
}
