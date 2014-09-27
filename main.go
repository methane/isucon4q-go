package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
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

func index(w http.ResponseWriter, req *http.Request) {
	sess := sessionStore.Get(req)
	buf := bytes.Buffer{}
	buf.WriteString(index_header)
	if sess.Notice != "" {
		buf.WriteString(`<div id="notice-message" class="alert alert-danger" role="alert">`)
		template.HTMLEscape(&buf, []byte(sess.Notice))
		buf.WriteString("</div>\n")
		sess.Notice = ""
		sessionStore.Set(w, sess)
	}
	buf.WriteString(index_footer)
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Write(buf.Bytes())
}

func login_post(w http.ResponseWriter, req *http.Request) {
	sess := sessionStore.Get(req)
	user, err := attemptLogin(req)

	if err != nil || user == nil {
		notice := ""
		switch err {
		case ErrBannedIP:
			notice = "You're banned."
		case ErrLockedUser:
			notice = "This account is locked."
		default:
			notice = "Wrong username or password"
		}
		sess.Notice = notice
		sessionStore.Set(w, sess)
		http.Redirect(w, req, "/", 302)
		return
	}
	sess.UserId = user.ID
	sessionStore.Set(w, sess)
	http.Redirect(w, req, "/mypage", 302)
}

func mypage(w http.ResponseWriter, req *http.Request) {
	sess := sessionStore.Get(req)
	var currentUser *User = nil
	if sess.UserId != 0 {
		currentUser = userRepository.ById(sess.UserId)
	}
	if currentUser == nil {
		sess.Notice = "You must be logged in"
		sessionStore.Set(w, sess)
		http.Redirect(w, req, "/", 302)
		return
	}
	currentUser.getLastLogin()
	loginAt := currentUser.LastLogin.CreatedAt.Format("2006-01-02 15:04:05")
	loginIp := currentUser.LastLogin.IP
	loginName := template.HTMLEscapeString(currentUser.LastLogin.Login)

	buf := bytes.Buffer{}
	buf.WriteString(mypage_header)
	fmt.Fprintf(&buf, `
  <dd id="last-logined-at">%s</dd>
  <dt>最終ログインIPアドレス</dt>
  <dd id="last-logined-ip">%s</dd>
</dl>

<div class="panel panel-default">
  <div class="panel-heading">
    お客様ご契約ID：%s 様の代表口座
`, loginAt, loginIp, loginName)
	buf.WriteString(mypage_footer)

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Write(buf.Bytes())

}

func main() {
	//m := Classic()

	//store := sessions.NewCookieStore([]byte("secret-isucon"))
	//m.Use(sessions.Sessions("isucon_go_session", store))
	//m.Use(render.Renderer())

	http.HandleFunc("/", index)
	http.HandleFunc("/login", login_post)
	//	m.Post("/login", func(req *http.Request, r render.Render, session sessions.Session) {
	//		user, err := attemptLogin(req)
	//
	//		notice := ""
	//		if err != nil || user == nil {
	//			switch err {
	//			case ErrBannedIP:
	//				notice = "You're banned."
	//			case ErrLockedUser:
	//				notice = "This account is locked."
	//			default:
	//				notice = "Wrong username or password"
	//			}
	//
	//			session.Set("notice", notice)
	//			r.Redirect("/")
	//			return
	//		}
	//
	//		session.Set("user_id", strconv.Itoa(user.ID))
	//		r.Redirect("/mypage")
	//	})

	http.HandleFunc("/mypage", mypage)
	//m.Get("/mypage", func(r render.Render, session sessions.Session) {
	//	var currentUser *User = nil
	//	sId := session.Get("user_id")
	//	userIdStr, ok := sId.(string)
	//	if ok {
	//		userId, err := strconv.Atoi(userIdStr)
	//		if err == nil {
	//			currentUser = userRepository.ById(userId)
	//		} else {
	//			log.Println(err)
	//		}
	//	} else {
	//		log.Printf("user_id = %#v (%T)", sId, sId)
	//	}
	//	if currentUser == nil {
	//		session.Set("notice", "You must be logged in")
	//		r.Redirect("/")
	//		return
	//	}
	//	currentUser.getLastLogin()
	//	r.HTML(200, "mypage", currentUser)
	//})

	//m.Get("/report", func(r render.Render) {
	//	r.JSON(200, map[string][]string{
	//		"banned_ips":   bannedIPs(),
	//		"locked_users": lockedUsers(),
	//	})
	//})
	http.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		data, err := json.Marshal(map[string][]string{
			"banned_ips":   bannedIPs(),
			"locked_users": lockedUsers(),
		})
		if err != nil {
			log.Println(err)
			w.WriteHeader(500)
			w.Write([]byte("error"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	http.HandleFunc("/__reset__", func(w http.ResponseWriter, r *http.Request) {
		initLogins()
		log.Println("reset")
		time.Sleep(time.Second)
		w.Write([]byte("OK"))
	})
	initStaticFiles("../public")

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

const mypage_header = `<!DOCTYPE html>
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
<div class="alert alert-success" role="alert">
  ログインに成功しました。<br>
  未読のお知らせが０件、残っています。
</div>

<dl class="dl-horizontal">
  <dt>前回ログイン</dt>
`

const mypage_footer = `
  </div>
  <div class="panel-body">
    <div class="row">
      <div class="col-sm-4">
        普通預金<br>
        <small>東京支店　1111111111</small><br>
      </div>
      <div class="col-sm-4">
        <p id="zandaka" class="text-right">
          ―――円
        </p>
      </div>

      <div class="col-sm-4">
        <p>
          <a class="btn btn-success btn-block">入出金明細を表示</a>
          <a class="btn btn-default btn-block">振込・振替はこちらから</a>
        </p>
      </div>

      <div class="col-sm-12">
        <a class="btn btn-link btn-block">定期預金・住宅ローンのお申込みはこちら</a>
      </div>
    </div>
  </div>
</div>
    </div>

  </body>
</html>
`
