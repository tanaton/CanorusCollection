package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/fcgi"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const HTML_START = `<!DOCTYPE html>
<html lang="ja">
<head>
<meta charset="utf-8">
<title>かっこう</title>
<meta http-equiv="content-style-type" content="text/css">
<meta http-equiv="content-script-type" content="text/javascript">
<style>
body {background-color:black;color:white;}
a:link, a:visited, a:active, a:hover {color:white;}
table {border:none;}
th, td {text-align:right;}
#allres {font-weight:700;font-size:1.5em;color:cyan;font-family:Arial;}
.top5 {font-size:1.5em;}
.top10 {font-size:1.3em;}
.top20 {font-size:1.1em;}
.server {font-weight:700;color:yellow;font-family:Arial;}
.line {}
</style>
</head>
<body>
<p>かっこう Ver {{.Ver}} = 板別発言数 本日 {{.NowDate}}</p>
<hr>
{{.Day}}の発言数 <span id="allres">{{.Allres}}</span> ({{.Date}} {{.Time}} 現在)
<hr>
{{.Message}}
<table>
<tr><th>順位</th><th>板名</th><th>- 本日の投稿数 -</th><th>- 本日のID数 -</th><th>- 新スレッド数 -</th><th>server</th></tr>
`

const HTML_END = `</table>
</body>
</html>`

const ITA_PATH = "/2ch_sc/dat/ita.data"
const COUNT_DATA_PATH = "/2ch_sc/scount.json"
const VER = "0.01b"

type SaveItem struct {
	Count  int
	Thread int
	Id     map[string]int
}

type ScItem struct {
	Board   string
	Server  string
	Res     int
	IdCount int
	Thread  int
}
type ScItems []*ScItem
type ScItemsByRes struct {
	ScItems
}

func (sc ScItems) Len() int      { return len(sc) }
func (sc ScItems) Swap(i, j int) { sc[i], sc[j] = sc[j], sc[i] }
func (scs ScItemsByRes) Less(i, j int) (ret bool) {
	// レス数降順
	if scs.ScItems[i].Res != scs.ScItems[j].Res {
		ret = scs.ScItems[i].Res > scs.ScItems[j].Res
	} else if scs.ScItems[i].IdCount != scs.ScItems[j].IdCount {
		ret = scs.ScItems[i].IdCount > scs.ScItems[j].IdCount
	} else if scs.ScItems[i].Thread != scs.ScItems[j].Thread {
		ret = scs.ScItems[i].Thread > scs.ScItems[j].Thread
	} else {
		ret = scs.ScItems[i].Board > scs.ScItems[j].Board
	}
	return
}

type HtmlStartOutput struct {
	Ver     string
	Day     string
	NowDate string
	Date    string
	Time    string
	Allres  string // カンマ区切り
	Message string
}

var g_reg_bbs = regexp.MustCompile(`(.+)\.2ch\.sc/(.+)<>`)
var gHtmlStart = template.Must(template.New("start").Parse(HTML_START))

func main() {
	fcgi.Serve(nil, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	out := HtmlStartOutput{
		Ver:     VER,
		NowDate: now.Format("2006/01/02"),
	}
	var path string

	datestr := r.URL.Query().Get("date")
	if datestr != "" {
		t, err := time.Parse("2006/01/02", datestr)
		if err == nil {
			path = createPath(t)
			out.Date = t.Format("2006/01/02")
			out.Time = "-"
			out.Day = out.Date
		} else {
			out.Message += "<p>書式が間違ってるっぽい？</p>"
		}
	}
	if path == "" {
		out.Day = "本日"
		path = createPath(now)
		if st, err := os.Stat(path); err == nil {
			t := st.ModTime()
			out.Date = t.Format("2006/01/02")
			out.Time = t.Format("15:04:05")
		} else {
			out.Date = "-"
			out.Time = "-"
		}
	}

	sclist := dataRead(path)
	allres := 0
	data := &bytes.Buffer{}
	for i, it := range sclist {
		var fsize string
		switch {
		case i >= 20:
			// スルー
		case i >= 10:
			fsize = ` class="top20"`
		case i >= 5:
			fsize = ` class="top10"`
		default:
			fsize = ` class="top5"`
		}
		fmt.Fprintf(data, "<tr>")
		fmt.Fprintf(data, `<td%s>%d</td>`, fsize, i+1)
		fmt.Fprintf(data, `<td%s>%s</td>`, fsize, it.Board)
		fmt.Fprintf(data, `<td%s>%s</td>`, fsize, commaNum(it.Res))
		fmt.Fprintf(data, `<td>%s</td>`, commaNum(it.IdCount))
		fmt.Fprintf(data, `<td>%s</td>`, commaNum(it.Thread))
		fmt.Fprintf(data, `<td class="server">%s</td>`, it.Server)
		fmt.Fprintf(data, "</tr>\n")
		allres += it.Res
	}
	out.Allres = commaNum(allres) // カンマ区切り
	if allres == 0 {
		out.Message += "<p>書き込みが無いっぽい？</p>"
	}
	w.Header().Set(`Content-Type`, `text/html; charset=utf-8`)

	// ステータスヘッダーの書き込み
	w.WriteHeader(http.StatusOK)
	// 本文の書き込み
	gHtmlStart.Execute(w, out)
	io.Copy(w, data)
	io.WriteString(w, HTML_END)
}

func dataRead(path string) ScItems {
	fp, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer fp.Close()
	r := bufio.NewReader(fp)
	data := map[string]*SaveItem{}
	json.NewDecoder(r).Decode(&data)
	sclist := make(ScItems, 0, len(data))

	bsm := getBoardServerNameMap()
	for board, it := range data {
		sc := &ScItem{
			Server:  bsm[board],
			Board:   board,
			Res:     it.Count,
			Thread:  it.Thread,
			IdCount: len(it.Id),
		}
		sclist = append(sclist, sc)
	}
	l := len(sclist)
	sclist = sclist[:l:l]
	// 並び替え
	sort.Sort(&ScItemsByRes{sclist})
	return sclist
}

func commaNum(res int) string {
	list := strings.Split(strconv.Itoa(res), "")
	l := len(list)
	ret := make([]string, 0, l+(l/3)+1)
	for i, j := 0, l-1; j >= 0; i, j = i+1, j-1 {
		ret = append(ret, list[i])
		if ((j % 3) == 0) && (j != 0) {
			ret = append(ret, ",")
		}
	}
	return strings.Join(ret, "")
}

func getBoardServerNameMap() map[string]string {
	bsm := make(map[string]string, 1024)
	fp, err := os.Open(ITA_PATH)
	if err != nil {
		return bsm
	}
	defer fp.Close()
	scanner := bufio.NewScanner(fp)
	for scanner.Scan() {
		if d := g_reg_bbs.FindStringSubmatch(scanner.Text()); len(d) > 0 {
			bsm[d[2]] = d[1]
		}
	}
	return bsm
}

func createPath(t time.Time) string {
	return COUNT_DATA_PATH + "/" + t.Format("2006_01_02") + ".json"
}
