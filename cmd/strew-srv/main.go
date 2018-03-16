// Copyright 2018 The strew Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"log"

	"github.com/sbinet-alt63/strew"
	_ "github.com/sbinet-alt63/strew/database/boltdb"
)

func main() {
	flag.Parse()

	if len(flag.Args()) != 1 {
		log.Fatalf("missing path to configuration file")
	}

	srv, err := strew.NewServerFrom(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	log.Fatal(srv.Serve(ctx))
}
