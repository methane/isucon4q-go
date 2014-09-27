package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"github.com/go-martini/martini"
	_ "github.com/go-sql-driver/mysql"
	"github.com/martini-contrib/render"
	"github.com/martini-contrib/sessions"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

var db *sql.DB
var (
	UserLockThreshold int
	IPBanThreshold    int
)

func init() {
	dsn := fmt.Sprintf(
		//"%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=Local",
		"%s:%s@unix(/var/lib/mysql/mysql.sock)/%s?parseTime=true&loc=Local",
		getEnv("ISU4_DB_USER", "root"),
		getEnv("ISU4_DB_PASSWORD", ""),
		//getEnv("ISU4_DB_HOST", "localhost"),
		//getEnv("ISU4_DB_PORT", "3306"),
		getEnv("ISU4_DB_NAME", "isu4_qualifier"),
	)

	var err error

	db, err = sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}
	db.SetMaxIdleConns(32)
	db.SetMaxOpenConns(32)
	initDb()

	UserLockThreshold, err = strconv.Atoi(getEnv("ISU4_USER_LOCK_THRESHOLD", "3"))
	if err != nil {
		panic(err)
	}

	IPBanThreshold, err = strconv.Atoi(getEnv("ISU4_IP_BAN_THRESHOLD", "10"))
	if err != nil {
		panic(err)
	}

	initUsers()
	initLogins()
}

// ClassicMartini represents a Martini with some reasonable defaults. Embeds the router functions for convenience.
type ClassicMartini struct {
	*martini.Martini
	martini.Router
}

// Classic creates a classic Martini with some basic default middleware - martini.Logger, martini.Recovery and martini.Static.
// Classic also maps martini.Routes as a service.
func Classic() *ClassicMartini {
	r := martini.NewRouter()
	m := martini.New()
	m.MapTo(r, (*martini.Routes)(nil))
	m.Action(r.Handle)
	return &ClassicMartini{m, r}
}

func index(w http.ResponseWriter, session sessions.Session) {
	buf := bytes.Buffer{}
	buf.WriteString(index_header)
	flush := getFlash(session, "notice")
	if len(flush) > 0 {
		buf.WriteString(`<div id="notice-message" class="alert alert-danger" role="alert">`)
		template.HTMLEscape(&buf, []byte(flush))
		buf.WriteString("</div>\n")
	}
	buf.WriteString(index_footer)

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Write(buf.Bytes())
}

func main() {
	m := Classic()

	store := sessions.NewCookieStore([]byte("secret-isucon"))
	m.Use(sessions.Sessions("isucon_go_session", store))

	m.Use(render.Renderer())

	m.Get("/", index)
	//m.Get("/", func(r render.Render, session sessions.Session) {
	//	r.HTML(200, "index", map[string]string{"Flash": getFlash(session, "notice")})
	//})

	m.Post("/login", func(req *http.Request, r render.Render, session sessions.Session) {
		user, err := attemptLogin(req)

		notice := ""
		if err != nil || user == nil {
			switch err {
			case ErrBannedIP:
				notice = "You're banned."
			case ErrLockedUser:
				notice = "This account is locked."
			default:
				notice = "Wrong username or password"
			}

			session.Set("notice", notice)
			r.Redirect("/")
			return
		}

		session.Set("user_id", strconv.Itoa(user.ID))
		r.Redirect("/mypage")
	})

	m.Get("/mypage", func(r render.Render, session sessions.Session) {
		var currentUser *User = nil
		sId := session.Get("user_id")
		userIdStr, ok := sId.(string)
		if ok {
			userId, err := strconv.Atoi(userIdStr)
			if err == nil {
				currentUser = userRepository.ById(userId)
			} else {
				log.Println(err)
			}
		} else {
			log.Printf("user_id = %#v (%T)", sId, sId)
		}
		if currentUser == nil {
			session.Set("notice", "You must be logged in")
			r.Redirect("/")
			return
		}
		currentUser.getLastLogin()
		r.HTML(200, "mypage", currentUser)
	})

	m.Get("/report", func(r render.Render) {
		r.JSON(200, map[string][]string{
			"banned_ips":   bannedIPs(),
			"locked_users": lockedUsers(),
		})
	})

	m.Get("/__reset__", func(r render.Render) {
		initLogins()
		r.Status(200)
	})

	initStaticFiles("../public")
	http.Handle("/", m)

	log.Println("Starting...")
	log.Fatal(http.ListenAndServe(":80", nil))
	//log.Fatal(http.ListenAndServe(":8080", m))
}

func initStaticFiles(prefix string) {
	wf := func(path string, info os.FileInfo, err error) error {
		log.Println(path, info, err)
		if path == prefix {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		urlpath := path[len(prefix):]
		if urlpath[0] != '/' {
			urlpath = "/" + urlpath
		}
		log.Println("Registering", urlpath, path)
		f, err := os.Open(path)
		if err != nil {
			log.Println(err)
			return nil
		}
		content := make([]byte, info.Size())
		f.Read(content)
		f.Close()

		handler := func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(path, ".css") {
				w.Header().Set("Content-Type", "text/css")
			} else if strings.HasSuffix(path, ".js") {
				w.Header().Set("Content-Type", "application/javascript")
			}
			w.Header().Set("Content-Length", strconv.FormatInt(int64(len(content)), 10))
			w.Write(content)
		}
		http.HandleFunc(urlpath, handler)
		return nil
	}
	filepath.Walk(prefix, wf)
}

const index_header = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="UTF-8">
    <link rel="stylesheet" href="/stylesheets/bootstrap.min.css">
    <link rel="stylesheet" href="/stylesheets/bootflat.min.css">
    <link rel="stylesheet" href="/stylesheets/isucon-bank.css">
    <title>isucon4</title>
  </head>
  <body>
    <div class="container">
      <h1 id="topbar">
        <a href="/"><img src="/images/isucon-bank.png" alt="いすこん銀行 オンラインバンキングサービス"></a>
      </h1>
<div id="be-careful-phising" class="panel panel-danger">
  <div class="panel-heading">
    <span class="hikaru-mozi">偽画面にご注意ください！</span>
  </div>
  <div class="panel-body">
    <p>偽のログイン画面を表示しお客様の情報を盗み取ろうとする犯罪が多発しています。</p>
    <p>ログイン直後にダウンロード中や、見知らぬウィンドウが開いた場合、<br>すでにウィルスに感染している場合がございます。即座に取引を中止してください。</p>
    <p>また、残高照会のみなど、必要のない場面で乱数表の入力を求められても、<br>絶対に入力しないでください。</p>
  </div>
</div>

<div class="page-header">
  <h1>ログイン</h1>
</div>
`

const index_footer = `
<div class="container">
  <form class="form-horizontal" role="form" action="/login" method="POST">
    <div class="form-group">
      <label for="input-username" class="col-sm-3 control-label">お客様ご契約ID</label>
      <div class="col-sm-9">
        <input id="input-username" type="text" class="form-control" placeholder="半角英数字" name="login">
      </div>
    </div>
    <div class="form-group">
      <label for="input-password" class="col-sm-3 control-label">パスワード</label>
      <div class="col-sm-9">
        <input type="password" class="form-control" id="input-password" name="password" placeholder="半角英数字・記号（２文字以上）">
      </div>
    </div>
    <div class="form-group">
      <div class="col-sm-offset-3 col-sm-9">
        <button type="submit" class="btn btn-primary btn-lg btn-block">ログイン</button>
      </div>
    </div>
  </form>
</div>
    </div>

  </body>
</html>
`
