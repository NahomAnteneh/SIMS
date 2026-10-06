package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/vmware/govmomi/simulator"
)

func main() {
	listenAddr := flag.String("l", ":8989", "listen address")
	flag.Parse()

	model := simulator.VPX()
	model.ServiceContent.About.Version = "8.0.3"
	model.ServiceContent.About.ApiVersion = "8.0.3.0"
	model.ServiceContent.About.FullName = "VMware vCenter Server 8.0.3 build-24022515"
	if err := model.Create(); err != nil {
		log.Fatalf("failed to create vcsim model: %v", err)
	}

	model.Service.RegisterEndpoints = true

	host, portStr, err := net.SplitHostPort(*listenAddr)
	if err != nil {
		host = ""
		portStr = "8989"
	}
	port, _ := strconv.Atoi(portStr)

	model.Service.TLS = new(tls.Config)
	model.Service.Listen = &url.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/sdk",
	}

	s := model.Service.NewServer()
	defer s.Close()

	log.Printf("SIMS vcsim (vSphere 8.0.3, API 8.0.3.0) started at %s", s.URL.String())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("Shutting down vcsim...")
}
