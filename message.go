// Copyright 2018 The strew Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package strew

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/mail"
	"time"
)

type Message struct {
	Subject     string
	From        string
	To          string
	Cc          string
	Bcc         string
	Date        string
	ID          string
	InReplyTo   string
	ContentType string
	XList       string
	Body        string
}

// Reply creates a new message that replies to this message
func (msg *Message) Reply() *Message {
	return &Message{
		Subject:   "Re: " + msg.Subject,
		To:        msg.From,
		InReplyTo: msg.ID,
		Date:      time.Now().Format(time.RFC3339),
	}
}

// ResendAs prepares a copy of the message being forwarded to a list
func (msg *Message) ResendAs(listID string, listAddress string) *Message {
	send := &Message{
		Subject:   msg.Subject,
		From:      msg.From,
		To:        msg.To,
		Cc:        msg.Cc,
		Date:      msg.Date,
		ID:        msg.ID,
		InReplyTo: msg.InReplyTo,
		XList:     listID + " <" + listAddress + ">",
	}

	// If the destination mailing list is in the Bcc field, keep it there
	bccList, err := mail.ParseAddressList(msg.Bcc)
	if err == nil {
		for _, bcc := range bccList {
			if bcc.Address == listAddress {
				send.Bcc = listID + " <" + listAddress + ">"
				break
			}
		}
	}
	return send
}

func (msg *Message) MarshalText() ([]byte, error) {
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "From: %s\r\n", msg.From)
	fmt.Fprintf(buf, "To: %s\r\n", msg.To)
	fmt.Fprintf(buf, "Cc: %s\r\n", msg.Cc)
	fmt.Fprintf(buf, "Bcc: %s\r\n", msg.Bcc)
	if len(msg.Date) > 0 {
		fmt.Fprintf(buf, "Date: %s\r\n", msg.Date)
	}
	if len(msg.ID) > 0 {
		fmt.Fprintf(buf, "Messsage-ID: %s\r\n", msg.ID)
	}
	fmt.Fprintf(buf, "In-Reply-To: %s\r\n", msg.InReplyTo)
	if len(msg.XList) > 0 {
		fmt.Fprintf(buf, "X-Mailing-List: %s\r\n", msg.XList)
		fmt.Fprintf(buf, "List-ID: %s\r\n", msg.XList)
		fmt.Fprintf(buf, "Sender: %s\r\n", msg.XList)
	}
	if len(msg.ContentType) > 0 {
		fmt.Fprintf(buf, "Content-Type: %s\r\n", msg.ContentType)
	}
	fmt.Fprintf(buf, "Subject: %s\r\n", msg.Subject)
	fmt.Fprintf(buf, "\r\n%s", msg.Body)

	return buf.Bytes(), nil
}

func (msg *Message) UnmarshalText(data []byte) error {
	_, err := msg.ReadFrom(bytes.NewReader(data))
	return err
}

func (msg *Message) ReadFrom(r io.Reader) (int64, error) {
	rr := creader{r: r}
	rmsg, err := mail.ReadMessage(&rr)
	if err != nil {
		return rr.n, err
	}

	// FIXME(sbinet): use io.ReadFull(r, []byte(max)) ?
	body, err := ioutil.ReadAll(rmsg.Body)
	if err != nil {
		return rr.n, err
	}

	msg.Subject = rmsg.Header.Get("Subject")
	msg.From = rmsg.Header.Get("From")
	msg.ID = rmsg.Header.Get("Message-ID")
	msg.InReplyTo = rmsg.Header.Get("In-Reply-To")
	msg.Body = string(body[:])
	msg.To = rmsg.Header.Get("To")
	msg.Cc = rmsg.Header.Get("Cc")
	msg.Bcc = rmsg.Header.Get("Bcc")
	msg.Date = rmsg.Header.Get("Date")

	return rr.n, nil
}

func (msg *Message) WriteTo(w io.Writer) (int64, error) {
	buf, err := msg.MarshalText()
	if err != nil {
		return 0, err
	}
	n, err := w.Write(buf)
	return int64(n), err
}

type creader struct {
	r io.Reader
	n int64
}

func (r *creader) Read(data []byte) (int, error) {
	n, err := r.r.Read(data)
	r.n += int64(n)
	return n, err
}
