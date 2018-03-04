// Copyright 2018 The strew Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package strew

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"net/smtp"
	"sort"
	"strings"

	bolt "github.com/coreos/bbolt"
	ini "gopkg.in/ini.v1"
)

var (
	subBucket = []byte("subscriptions")

	errInvalidListID = errors.New("strew: invalid list ID")
)

// Server is a mailing list server.
type Server struct {
	cfg Config
	db  *bolt.DB
	msg chan *Message
}

func NewServerFrom(fname string) (*Server, error) {
	cfg, err := newConfig(fname)
	if err != nil {
		return nil, err
	}
	return NewServer(cfg)
}

func NewServer(cfg Config) (*Server, error) {
	db, err := bolt.Open(cfg.Database, 0600, nil)
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(subBucket)
		if err != nil {
			return err
		}
		for _, list := range cfg.Lists {
			id := []byte(list.ID)
			vs := b.Get(id)
			err := b.Put(id, vs)
			if err != nil {
				return err
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}

	client, err := smtp.Dial(cfg.SMTPHostname + ":" + cfg.SMTPPort)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	auth := smtp.PlainAuth(
		"",
		cfg.SMTPUsername, cfg.SMTPPassword,
		cfg.SMTPHostname,
	)
	err = client.StartTLS(&tls.Config{
		ServerName: cfg.SMTPHostname,
	})
	if err != nil {
		return nil, err
	}

	err = client.Auth(auth)
	if err != nil {
		return nil, err
	}

	return &Server{cfg: cfg, db: db, msg: make(chan *Message)}, nil
}

func (srv *Server) Msg() chan *Message {
	return srv.msg
}

func (srv *Server) Serve(ctx context.Context) error {
	for {
		select {
		case msg := <-srv.msg:
			var err error
			switch {
			case srv.isCommand(msg):
				err = srv.handleCommand(ctx, msg)
			default:
				err = srv.handleMessage(ctx, msg)
			}
			if err != nil {
				// FIXME(sbinet): better handling
				log.Print(err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (srv *Server) isCommand(msg *Message) bool {
	for _, list := range []string{msg.To, msg.Cc, msg.Bcc} {
		addrs, err := mail.ParseAddressList(list)
		if err != nil {
			continue
		}
		for _, v := range addrs {
			if v.Address == srv.cfg.CommandAddress {
				return true
			}
		}
	}
	return false
}

func (srv *Server) handleCommand(ctx context.Context, msg *Message) error {
	switch {
	case msg.Subject == "lists":
		return srv.handleShowLists(ctx, msg)
	case msg.Subject == "help":
		return srv.handleHelp(ctx, msg)
	case msg.Subject == "subscriptions":
		return srv.handleShowSubscriptions(ctx, msg)
	case strings.HasPrefix(msg.Subject, "subscribe"):
		return srv.handleSubscribe(ctx, msg)
	case strings.HasPrefix(msg.Subject, "unsubscribe"):
		return srv.handleUnsubscribe(ctx, msg)
	default:
		return srv.handleUnknownCommand(ctx, msg)
	}
}

func (srv *Server) handleShowLists(ctx context.Context, msg *Message) error {
	body := new(bytes.Buffer)
	fmt.Fprintf(body, "Available mailing lists:\r\n\r\n")
	for _, list := range srv.cfg.Lists {
		if list.Hidden {
			continue
		}
		fmt.Fprintf(body,
			"ID: %s\r\n"+
				"Name: %s\r\n"+
				"Description: %s\r\n"+
				"Address: %s\r\n\r\n",
			list.ID, list.Name, list.Description, list.Address,
		)
	}

	fmt.Fprintf(body,
		"\r\nTo subscribe to a mailing list, email %s with 'subscribe <list-id>' as the subject.\r\n",
		srv.cfg.CommandAddress,
	)

	reply := msg.Reply()
	reply.From = srv.cfg.CommandAddress
	reply.Body = body.String()

	return srv.send(reply, []string{msg.From})
}

func (srv *Server) handleHelp(ctx context.Context, msg *Message) error {
	body := new(bytes.Buffer)
	fmt.Fprintf(body, srv.commandInfo())
	reply := msg.Reply()
	reply.From = srv.cfg.CommandAddress
	reply.Body = body.String()
	return srv.send(reply, []string{msg.From})
}

func (srv *Server) handleShowSubscriptions(ctx context.Context, msg *Message) error {
	body := new(bytes.Buffer)
	fmt.Fprintf(body, "Mailing lists:\r\n\r\n")
	for _, list := range srv.cfg.Lists {
		if list.Hidden {
			continue
		}
		if !srv.isSubscribed(msg.From, list.ID) {
			continue
		}

		fmt.Fprintf(body,
			"ID: %s\r\n"+
				"Name: %s\r\n"+
				"Description: %s\r\n"+
				"Address: %s\r\n\r\n",
			list.ID, list.Name, list.Description, list.Address,
		)
	}

	fmt.Fprintf(body,
		"\r\nTo subscribe to a mailing list, email %s with 'subscribe <list-id>' as the subject.\r\n",
		srv.cfg.CommandAddress,
	)

	reply := msg.Reply()
	reply.From = srv.cfg.CommandAddress
	reply.Body = body.String()

	return srv.send(reply, []string{msg.From})
}

func (srv *Server) handleSubscribe(ctx context.Context, msg *Message) error {
	listID := strings.TrimPrefix(msg.Subject, "subscribe ")
	list := srv.lookupList(listID)

	if list == nil {
		reply := msg.Reply()
		reply.Body = fmt.Sprintf("Unable to subscribe to %s  - it is not a valid mailing list.\r\n", listID)
		return srv.send(reply, []string{msg.From})
	}

	// Switch to id - in case we were passed address
	listID = list.ID

	if srv.isSubscribed(msg.From, listID) {
		reply := msg.Reply()
		reply.Body = fmt.Sprintf("You are already subscribed to %s\r\n", listID)
		return srv.send(reply, []string{msg.From})
	}

	err := srv.subscribe(msg.From, listID)
	if err != nil {
		return err
	}

	reply := msg.Reply()
	reply.Body = fmt.Sprintf("You are now subscribed to %s\r\n", listID)
	return srv.send(reply, []string{msg.From})
}

func (srv *Server) handleUnsubscribe(ctx context.Context, msg *Message) error {
	listID := strings.TrimPrefix(msg.Subject, "unsubscribe ")
	list := srv.lookupList(listID)

	if list == nil {
		reply := msg.Reply()
		reply.Body = fmt.Sprintf("Unable to unsubscribe from %s  - it is not a valid mailing list.\r\n", listID)
		return srv.send(reply, []string{msg.From})
	}

	// Switch to id - in case we were passed address
	listID = list.ID

	if !srv.isSubscribed(msg.From, listID) {
		reply := msg.Reply()
		reply.Body = fmt.Sprintf("You aren't subscribed to %s\r\n", listID)
		return srv.send(reply, []string{msg.From})
	}

	err := srv.unsubscribe(msg.From, listID)
	if err != nil {
		return err
	}

	reply := msg.Reply()
	reply.Body = fmt.Sprintf("You are now unsubscribed from %s\r\n", listID)
	return srv.send(reply, []string{msg.From})
}

func (srv *Server) handleUnknownCommand(ctx context.Context, msg *Message) error {
	reply := msg.Reply()
	reply.From = srv.cfg.CommandAddress
	reply.Body = fmt.Sprintf(
		"%s is not a valid command.\r\n\r\n"+
			"Valid commands are:\r\n\r\n"+
			srv.commandInfo(),
		msg.Subject,
	)
	return srv.send(reply, []string{msg.From})
}

func (srv *Server) handleMessage(ctx context.Context, msg *Message) error {
	lists := srv.lookupLists(msg)
	if len(lists) == 0 {
		return srv.handleNoDestination(ctx, msg)
	}

	var last error
	for _, list := range lists {
		if !srv.canPost(msg.From, list) {
			err := srv.handleNotAuthorizedToPost(ctx, msg, list)
			if err != nil {
				last = err
			}
			continue
		}
		fwd := msg.ResendAs(list.ID, list.Address)
		err := srv.sendList(fwd, list)
		if err != nil {
			last = err
		}
	}
	return last
}

func (srv *Server) handleNoDestination(ctx context.Context, msg *Message) error {
	reply := msg.Reply()
	reply.From = srv.cfg.CommandAddress
	reply.Body = "No mailing lists addressed. Your message has not been delivered.\r\n"
	return srv.send(reply, []string{msg.From})
}

func (srv *Server) handleNotAuthorizedToPost(ctx context.Context, msg *Message, list *List) error {
	reply := msg.Reply()
	reply.From = srv.cfg.CommandAddress
	reply.Body = fmt.Sprintf("You are not an approved poster for this mailing list (%s). Your message has not been delivered.\r\n", list.Address)

	return srv.send(reply, []string{msg.From})
}

func (srv *Server) lookupLists(msg *Message) []*List {
	var lists []*List
	for _, addrs := range []string{msg.To, msg.Cc, msg.Bcc} {
		addr, err := mail.ParseAddressList(addrs)
		if err != nil {
			continue
		}
		for _, v := range addr {
			list := srv.lookupList(v.Address)
			if list != nil {
				lists = append(lists, list)
			}
		}
	}
	return lists
}

func (srv *Server) lookupList(key string) *List {
	for _, list := range srv.cfg.Lists {
		switch key {
		case list.ID, list.Address:
			return list
		}
	}
	return nil
}

func (srv *Server) canPost(from string, list *List) bool {
	if list.SubscribersOnly && !srv.isSubscribed(from, list.ID) {
		return false
	}

	// Is there a whitelist of approved posters?
	if len(list.Posters) > 0 {
		for _, poster := range list.Posters {
			if from == poster {
				return true
			}
		}
		return false
	}

	return true
}

func (srv *Server) sendList(msg *Message, list *List) error {
	recipients, err := srv.subscribers(list.ID)
	if err != nil {
		return err
	}
	recipients = append(recipients, list.Bcc...)

	return srv.send(msg, recipients)
}

func (srv *Server) send(msg *Message, recipients []string) error {
	body, err := msg.MarshalText()
	if err != nil {
		return err
	}

	return smtp.SendMail(
		srv.cfg.SMTPHostname+":"+srv.cfg.SMTPPort,
		smtp.PlainAuth("",
			srv.cfg.SMTPUsername, srv.cfg.SMTPPassword,
			srv.cfg.SMTPHostname,
		),
		msg.From, recipients,
		body,
	)
}

// subscribers returns the list of subscribers for the given mailing list ID.
func (srv *Server) subscribers(list string) ([]string, error) {
	var (
		users []string
		key   = []byte(list)
	)

	err := srv.db.View(func(tx *bolt.Tx) error {
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

// subscribe subscribes a user to a mailing list
func (srv *Server) subscribe(user, list string) error {
	k := []byte(list)
	return srv.db.Update(func(tx *bolt.Tx) error {
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

// unsubscribe removes a user from the given mailing list.
func (srv *Server) unsubscribe(user, list string) error {
	k := []byte(list)
	return srv.db.Update(func(tx *bolt.Tx) error {
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

func (srv *Server) isSubscribed(user, list string) bool {
	var o bool
	usr := []byte(user)
	key := []byte(list)
	err := srv.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(subBucket)
		v := b.Get(key)
		for _, v := range bytes.Split(v, []byte(",")) {
			if bytes.Equal(v, usr) {
				o = true
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return false
	}
	return o
}

// commandInfo generates an email-able list of commands
func (srv *Server) commandInfo() string {
	return fmt.Sprintf("    help\r\n"+
		"      Information about valid commands\r\n"+
		"\r\n"+
		"    list\r\n"+
		"      Retrieve a list of available mailing lists\r\n"+
		"\r\n"+
		"    subscriptions\r\n"+
		"      Retrieve a list of subscribed mailing lists\r\n"+
		"\r\n"+
		"    subscribe <list-id>\r\n"+
		"      Subscribe to <list-id>\r\n"+
		"\r\n"+
		"    unsubscribe <list-id>\r\n"+
		"      Unsubscribe from <list-id>\r\n"+
		"\r\n"+
		"To send a command, email %s with the command as the subject.\r\n",
		srv.cfg.CommandAddress,
	)
}

type Config struct {
	CommandAddress string `ini:"command_address"`
	Log            string `ini:"log"`
	Database       string `ini:"database"`
	SMTPHostname   string `ini:"smtp_hostname"`
	SMTPPort       string `ini:"smtp_port"`
	SMTPUsername   string `ini:"smtp_username"`
	SMTPPassword   string `ini:"smtp_password"`
	Lists          map[string]*List
	Debug          bool
}

func newConfig(fname string) (Config, error) {
	var cfg Config
	f, err := ini.Load(fname)
	if err != nil {
		return cfg, err
	}

	err = f.Section("").MapTo(&cfg)
	if err != nil {
		return cfg, err
	}

	cfg.Lists = make(map[string]*List)
	for _, section := range f.ChildSections("list") {
		var list List
		err = section.MapTo(&list)
		if err != nil {
			return cfg, err
		}
		list.ID = strings.TrimPrefix(section.Name(), "list.")
		cfg.Lists[list.Address] = &list
	}
	return cfg, nil
}

type List struct {
	ID              string
	Name            string   `ini:"name"`
	Description     string   `ini:"description"`
	Address         string   `ini:"address"`
	Hidden          bool     `ini:"hidden"`
	SubscribersOnly bool     `ini:"subscribers_only"`
	Posters         []string `ini:"posters,omitempty"`
	Bcc             []string `ini:"bcc,omitempty"`
}

type byteSlice [][]byte

func (p byteSlice) Len() int      { return len(p) }
func (p byteSlice) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p byteSlice) Less(i, j int) bool {
	return bytes.Compare(p[i], p[j]) == -1
}