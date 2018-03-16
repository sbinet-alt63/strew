// Copyright 2018 The strew Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package database defines the interface to access lists definitions
// and users subscriptions.
package database

import (
	"errors"
	"sort"
	"sync"
)

// Store defines how to interact with a concrete database.
type Store interface {
	AddList(list string) error
	DelList(list string) error
	Subscribers(list string) ([]string, error)
	Subscribe(user, list string) error
	Unsubscribe(user, list string) error
	Lists() ([]string, error)
	Users() ([]string, error)
}

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]Driver)
)

var (
	ErrUnknownDriver = errors.New("strew/database: unknown driver name")
)

// Open opens a database specified by its database driver name and a
// driver-specific data source name, usually consisting of at least a database
// name and connection information.
func Open(driverName, dataSourceName string) (Store, error) {
	driversMu.RLock()
	driveri, ok := drivers[driverName]
	driversMu.RUnlock()
	if !ok {
		return nil, ErrUnknownDriver
	}
	return driveri(dataSourceName)
}

// Driver is a function that creates new Stores.
type Driver func(src string) (Store, error)

// Register makes a database driver available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(name string, driver Driver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	if driver == nil {
		panic("strew/database: Register driver is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("strew/database: Register called twice for driver " + name)
	}
	drivers[name] = driver
}

// Drivers returns a sorted list of the names of the registered drivers.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	var list []string
	for name := range drivers {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}
