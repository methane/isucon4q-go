package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	ErrBannedIP      = errors.New("Banned IP")
	ErrLockedUser    = errors.New("Locked user")
	ErrUserNotFound  = errors.New("Not found user")
	ErrWrongPassword = errors.New("Wrong password")
)

type UserLogin struct {
	Id        int
	Ip        string
	Login     string
	Success   bool
	CreatedAt time.Time
}

type LoginHistory struct {
	sync.RWMutex
	byName map[string][]*UserLogin
	byAddr map[string][]*UserLogin
}

func NewLoginHistory() *LoginHistory {
	return &LoginHistory{
		byName: make(map[string][]*UserLogin),
		byAddr: make(map[string][]*UserLogin),
	}
}

func (h *LoginHistory) ByName(name string) []*UserLogin {
	h.RLock()
	r := h.byName[name]
	h.RUnlock()
	return r
}

func (h *LoginHistory) ByAddr(addr string) []*UserLogin {
	h.RLock()
	r := h.byAddr[addr]
	h.RUnlock()
	return r
}

func (h *LoginHistory) Add(login *UserLogin) {
	h.Lock()
	h.add(login)
	h.Unlock()
}

func (h *LoginHistory) add(login *UserLogin) {
	h.byName[login.Login] = append(h.byName[login.Login], login)
	h.byAddr[login.Ip] = append(h.byAddr[login.Ip], login)
}

var loginHistory *LoginHistory

func initLogins() {
	loginHistory = NewLoginHistory()

	rows, err := db.Query("SELECT `user_id`, `ip`, `login`, `succeeded`, `created_at` FROM login_log")
	must(err)
	defer rows.Close()
	for rows.Next() {
		login := &UserLogin{}
		err := rows.Scan(&login.Id, &login.Ip, &login.Login, &login.Success, &login.CreatedAt)
		//log.Printf("%+v", login)
		must(err)
		loginHistory.Add(login)
	}
}

var insertStmt *sql.Stmt
var insertCh chan *UserLogin

func initDb() {
	var err error
	insertStmt, err = db.Prepare(
		"INSERT INTO login_log (`created_at`, `user_id`, `login`, `ip`, `succeeded`) " +
			"VALUES (?,?,?,?,?)")
	must(err)

	insertCh = make(chan *UserLogin, 5)
	go inserter()
	go inserter()
	go inserter()
	go inserter()
}

func inserter() {
	for {
		l := <-insertCh
		_, err := insertStmt.Exec(l.CreatedAt, l.Id, l.Login, l.Ip, l.Success)
		if err != nil {
			log.Println(err)
		}
	}
}

func createLoginLog(succeeded bool, remoteAddr, login string, user *User) error {
	now := time.Now()
	ul := &UserLogin{Id: user.ID, Ip: remoteAddr, Login: login, Success: succeeded, CreatedAt: now}
	loginHistory.Add(ul)
	insertCh <- ul
	return nil
}

func isLockedUser(user *User) (bool, error) {
	if user == nil {
		return false, nil
	}
	hi := loginHistory.ByName(user.Login)
	if hi == nil || len(hi) < UserLockThreshold {
		return false, nil
	}

	c := 0
	for i := len(hi) - 1; i >= 0; i-- {
		h := hi[i]
		if h.Success {
			return false, nil
		}
		c++
		if c >= UserLockThreshold {
			return true, nil
		}
	}
	return false, nil

	//var ni sql.NullInt64
	//row := db.QueryRow(
	//	"SELECT COUNT(1) AS failures FROM login_log WHERE "+
	//		"user_id = ? AND id > IFNULL((select id from login_log where user_id = ? AND "+
	//		"succeeded = 1 ORDER BY id DESC LIMIT 1), 0);",
	//	user.ID, user.ID,
	//)
	//err := row.Scan(&ni)

	//switch {
	//case err == sql.ErrNoRows:
	//	return false, nil
	//case err != nil:
	//	return false, err
	//}

	//return UserLockThreshold <= int(ni.Int64), nil
}

func isBannedIP(ip string) (bool, error) {
	hi := loginHistory.ByAddr(ip)
	if hi == nil || len(hi) < IPBanThreshold {
		return false, nil
	}
	c := 0
	for i := len(hi) - 1; i >= 0; i-- {
		h := hi[i]
		if h.Success {
			return false, nil
		}
		c++
		if c >= IPBanThreshold {
			return true, nil
		}
	}
	return false, nil
	//var ni sql.NullInt64
	//row := db.QueryRow(
	//	"SELECT COUNT(1) AS failures FROM login_log WHERE "+
	//		"ip = ? AND id > IFNULL((select id from login_log where ip = ? AND "+
	//		"succeeded = 1 ORDER BY id DESC LIMIT 1), 0);",
	//	ip, ip,
	//)
	//err := row.Scan(&ni)
	//switch {
	//case err == sql.ErrNoRows:
	//	return false, nil
	//case err != nil:
	//	return false, err
	//}
	//return IPBanThreshold <= int(ni.Int64), nil
}

func attemptLogin(req *http.Request) (*User, error) {
	succeeded := false

	loginName := req.PostFormValue("login")
	password := req.PostFormValue("password")

	remoteAddr := req.RemoteAddr
	if xForwardedFor := req.Header.Get("X-Forwarded-For"); len(xForwardedFor) > 0 {
		remoteAddr = xForwardedFor
	}

	user := userRepository.ByName(loginName)
	defer func() {
		createLoginLog(succeeded, remoteAddr, loginName, user)
	}()

	if banned, _ := isBannedIP(remoteAddr); banned {
		return nil, ErrBannedIP
	}
	if locked, _ := isLockedUser(user); locked {
		return nil, ErrLockedUser
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	if user.password != password {
		return nil, ErrWrongPassword
	}
	succeeded = true
	return user, nil
}

func bannedIPs() []string {
	ips := []string{}

	rows, err := db.Query(
		"SELECT ip FROM "+
			"(SELECT ip, MAX(succeeded) as max_succeeded, COUNT(1) as cnt FROM login_log GROUP BY ip) "+
			"AS t0 WHERE t0.max_succeeded = 0 AND t0.cnt >= ?",
		IPBanThreshold,
	)

	if err != nil {
		return ips
	}

	defer rows.Close()
	for rows.Next() {
		var ip string

		if err := rows.Scan(&ip); err != nil {
			return ips
		}
		ips = append(ips, ip)
	}
	if err := rows.Err(); err != nil {
		return ips
	}

	rowsB, err := db.Query(
		"SELECT ip, MAX(id) AS last_login_id FROM login_log WHERE succeeded = 1 GROUP by ip",
	)

	if err != nil {
		return ips
	}

	defer rowsB.Close()
	for rowsB.Next() {
		var ip string
		var lastLoginId int

		if err := rows.Scan(&ip, &lastLoginId); err != nil {
			return ips
		}

		var count int

		err = db.QueryRow(
			"SELECT COUNT(1) AS cnt FROM login_log WHERE ip = ? AND ? < id",
			ip, lastLoginId,
		).Scan(&count)

		if err != nil {
			return ips
		}

		if IPBanThreshold <= count {
			ips = append(ips, ip)
		}
	}
	if err := rowsB.Err(); err != nil {
		return ips
	}

	return ips
}

func lockedUsers() []string {
	userIds := []string{}

	rows, err := db.Query(
		"SELECT user_id, login FROM "+
			"(SELECT user_id, login, MAX(succeeded) as max_succeeded, COUNT(1) as cnt FROM login_log GROUP BY user_id) "+
			"AS t0 WHERE t0.user_id IS NOT NULL AND t0.max_succeeded = 0 AND t0.cnt >= ?",
		UserLockThreshold,
	)

	if err != nil {
		return userIds
	}

	defer rows.Close()
	for rows.Next() {
		var userId int
		var login string

		if err := rows.Scan(&userId, &login); err != nil {
			return userIds
		}
		userIds = append(userIds, login)
	}
	if err := rows.Err(); err != nil {
		return userIds
	}

	rowsB, err := db.Query(
		"SELECT user_id, login, MAX(id) AS last_login_id FROM login_log WHERE user_id IS NOT NULL AND succeeded = 1 GROUP BY user_id",
	)

	if err != nil {
		return userIds
	}

	defer rowsB.Close()
	for rowsB.Next() {
		var userId int
		var login string
		var lastLoginId int

		if err := rowsB.Scan(&userId, &login, &lastLoginId); err != nil {
			return userIds
		}

		var count int

		err = db.QueryRow(
			"SELECT COUNT(1) AS cnt FROM login_log WHERE user_id = ? AND ? < id",
			userId, lastLoginId,
		).Scan(&count)

		if err != nil {
			return userIds
		}

		if UserLockThreshold <= count {
			userIds = append(userIds, login)
		}
	}
	if err := rowsB.Err(); err != nil {
		return userIds
	}

	return userIds
}
