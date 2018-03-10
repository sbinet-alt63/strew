package database

import (
	"bytes"
	"errors"
	bolt "github.com/coreos/bbolt"
	"sort"
)

// BoltDataStore contains methods that deal with BoltDB
type BoltDataStore struct {
	Db *bolt.DB
}
type byteSlice [][]byte

var (
	subBucket        = []byte("subscriptions")
	errInvalidListID = errors.New("strew: invalid list ID")
	// TODO: Change "bucket not found" to something domain related
	errInvalidBucket = errors.New("strew: bucket not found")
)

// New returns an instance of BoltDataStore.
func New(db *bolt.DB) *BoltDataStore {
	return &BoltDataStore{Db: db}
}

// Subscribe adds given user to mailing list
func (b *BoltDataStore) Subscribe(user, list string) error {
	k := []byte(list)
	return b.Db.Update(func(tx *bolt.Tx) error {
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

// Subscribers returns the list of subscribers for given mailing list.
func (b *BoltDataStore) Subscribers(list string) ([]string, error) {

	var (
		users []string
		key   = []byte(list)
	)

	err := b.Db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(subBucket)
		v := b.Get(key)
		if v == nil {
			return errInvalidListID
		}
		vs := bytes.Split(v, []byte(","))
		for _, v := range vs {
			users = append(users, string(v))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return users, nil
}

// Unsubscribe removes a user from the given mailing list.
func (b *BoltDataStore) Unsubscribe(user, list string) error {
	k := []byte(list)
	return b.Db.Update(func(tx *bolt.Tx) error {
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

// Lists returns the list of mailing lists.
func (b *BoltDataStore) Lists() ([]string, error) {
	var mailingLists []string
	err := b.Db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(subBucket))
		if b == nil {
			return errInvalidBucket
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			mailingLists = append(mailingLists, string(k))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mailingLists, nil
}

// Users returns list of subscribers in all mailing list
func (b *BoltDataStore) Users(db *bolt.DB) ([]string, error) {
	mailingLists, err := b.Lists()
	if err != nil {
		return nil, err
	}
	var users []string
	// Iterating through list of mailingLists
	for _, topic := range mailingLists {
		subscribers, _ := b.Subscribers(topic)
		if err == nil {
			users = append(users, subscribers...)
		}
	}
	return users, nil
}

func (p byteSlice) Len() int      { return len(p) }
func (p byteSlice) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p byteSlice) Less(i, j int) bool {
	return bytes.Compare(p[i], p[j]) == -1
}
