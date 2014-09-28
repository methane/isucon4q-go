package main

import (
	"./sessions"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
)

const sessionName = "isucon_session"

type Session struct {
	UserId int
	Key    string
	Notice string
}

type SessionStore struct {
	sync.Mutex
	store map[string]*Session
}

var sessionStore = SessionStore{
	store: make(map[string]*Session),
}

func (self *SessionStore) Get(r *http.Request) *Session {
	cookie, _ := r.Cookie(sessionName)
	if cookie == nil {
		return &Session{}
	}
	key := cookie.Value
	self.Lock()
	s := self.store[key]
	self.Unlock()
	if s == nil {
		s = &Session{}
	}
	return s
}

func (self *SessionStore) Set(w http.ResponseWriter, sess *Session) {
	key := sess.Key
	if key == "" {
		b := make([]byte, 8)
		rand.Read(b)
		key = hex.EncodeToString(b)
		sess.Key = key
	}

	cookie := sessions.NewCookie(sessionName, key, &sessions.Options{})
	http.SetCookie(w, cookie)

	self.Lock()
	self.store[key] = sess
	self.Unlock()
}
