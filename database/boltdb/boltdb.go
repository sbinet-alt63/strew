// Copyright 2018 The strew Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package boltdb implements a database.Store backed by bolt.
package boltdb

import (
	"bytes"
	"sort"

	bolt "github.com/coreos/bbolt"
	"github.com/pkg/errors"
	"github.com/sbinet-alt63/strew/database"
)

var (
	subBucket = []byte("subscriptions")
	lstBucket = []byte("lists")

	errInvalidListID     = errors.New("strew/database/boltdb: invalid list ID")
	errInvalidListBucket = errors.New("strew/database/boltdb: invalid list bucket")
)

type store struct {
	db *bolt.DB
}

func (db *store) AddList(list string) error {
	k := []byte(list)
	return db.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(lstBucket)
		return b.Put(k, []byte("1"))
	})
}

func (db *store) DelList(list string) error {
	k := []byte(list)
	return db.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(lstBucket)
		return b.Put(k, []byte("0"))
	})
}

func (db *store) Subscribers(list string) ([]string, error) {
	var (
		users []string
		key   = []byte(list)
	)

	err := db.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(subBucket)
		v := b.Get(key)
		if v == nil {
			return errors.WithStack(errInvalidListID)
		}
		vs := bytes.Split(v, []byte(","))
		for _, v := range vs {
			users = append(users, string(v))
		}
		return nil
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return users, nil
}

func (db *store) Subscribe(user, list string) error {
	k := []byte(list)
	return db.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(subBucket)
		v := b.Get(k)
		vs := bytes.Split(v, []byte(","))
		user := []byte(user)
		users := make([][]byte, 0, len(vs)+1)
		for _, v := range vs {
			if !bytes.Equal(v, user) {
				users = append(users, v)
			}
		}
		users = append(users, user)
		sort.Sort(byteSlice(users))

		return b.Put(k, bytes.Join(users, []byte(",")))
	})
}

func (db *store) Unsubscribe(user, list string) error {
	k := []byte(list)
	return db.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(subBucket)
		v := b.Get(k)
		vs := bytes.Split(v, []byte(","))
		user := []byte(user)
		users := make([][]byte, 0, len(vs))
		for _, v := range vs {
			if !bytes.Equal(v, user) {
				users = append(users, v)
			}
		}
		sort.Sort(byteSlice(users))
		return b.Put(k, bytes.Join(users, []byte(",")))
	})
}

func (db *store) Lists() ([]string, error) {
	var (
		lists []string
	)

	err := db.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(lstBucket)
		return b.ForEach(func(k, v []byte) error {
			if bytes.Equal(v, []byte("1")) {
				lists = append(lists, string(k))
			}
			return nil
		})
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return lists, nil
}

func (db *store) Users() ([]string, error) {
	panic("not implemented")
}

func init() {
	database.Register("boltdb", func(src string) (database.Store, error) {
		db, err := bolt.Open(src, 0600, nil)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		for _, bckt := range [][]byte{
			subBucket,
			lstBucket,
		} {
			err = db.Update(func(tx *bolt.Tx) error {
				_, err := tx.CreateBucketIfNotExists(bckt)
				if err != nil {
					return errors.WithStack(err)
				}
				return nil
			})
			if err != nil {
				return nil, errors.WithMessage(err, string(bckt))
			}
		}
		return &store{db: db}, nil
	})
}

type byteSlice [][]byte

func (p byteSlice) Len() int      { return len(p) }
func (p byteSlice) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p byteSlice) Less(i, j int) bool {
	return bytes.Compare(p[i], p[j]) == -1
}

var (
	_ database.Store = (*store)(nil)
)
