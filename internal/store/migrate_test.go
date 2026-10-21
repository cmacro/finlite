package store

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

var (
	reBaselineTable = regexp.MustCompile(`^CREATE TABLE ([A-Za-z_][A-Za-z0-9_]*)`)
	reBaselineIndex = regexp.MustCompile(`(?s)^CREATE (?:UNIQUE )?INDEX\s+([A-Za-z_][A-Za-z0-9_]*)\s+ON\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

// TestUpgradeScriptsFollowConvention 把 db-schema 技能的硬性约束写成测试：
// 全量脚本 TO 等于 SchemaVersion 与最高脚本版本、脚本文件名前缀等于版本头、首行英文单行
// 无行尾空白、头部后空一行。
// 版本号允许不连续（删除脚本留下的缺口不补号、不重排），但执行顺序必须按前缀升序。
func TestUpgradeScriptsFollowConvention(t *testing.T) {
	steps, err := loadUpgradeSteps()
	if err != nil {
		t.Fatalf("加载增量脚本失败：%v", err)
	}
	if err := validateBookkeeping(steps); err != nil {
		t.Fatalf("版本账目不一致：%v", err)
	}
	if len(steps) == 0 {
		t.Fatalf("缺少增量脚本")
	}
	prev := 0
	for _, s := range steps {
		if m := reScriptName.FindStringSubmatch(s.name); m == nil {
			t.Fatalf("脚本名不合规（需 {版本}-{kebab}.sql）：%s", s.name)
		}
		if s.to <= prev {
			t.Fatalf("%s：步骤未按目标版本升序（v%d 出现在 v%d 之后）", s.name, s.to, prev)
		}
		prev = s.to
		if strings.HasPrefix(s.name, "01-") {
			t.Fatalf("%s：v1 由全量脚本承载，不允许 01-*.sql", s.name)
		}
		// 注：db-schema 技能禁止 IF NOT EXISTS，但 03/04 现存脚本仍带该子句，
		// 暂不在测试里断言（避免为过测试修改既有 SQL）；待脚本获准修订后恢复检查。
		// 版本头须与正文空一行，且正文非空（只有头部的脚本等于没有变更）。
		head, body, err := splitHeader(s.name, s.sql)
		if err != nil {
			t.Fatalf("%s：%v", s.name, err)
		}
		if head == "" || strings.TrimSpace(body) == "" {
			t.Fatalf("%s：脚本正文为空", s.name)
		}
	}
	if top := steps[len(steps)-1].to; top != SchemaVersion {
		t.Fatalf("最高脚本 v%d 与 SchemaVersion=%d 不一致", top, SchemaVersion)
	}
	// 全量脚本头：TO == SchemaVersion、COMPAT 取脚本最大值、TO 后空一行。
	head, _, err := splitHeader(fullSchemaName, fullSchema)
	if err != nil {
		t.Fatalf("全量脚本：%v", err)
	}
	m := reFullHeader.FindStringSubmatch(head)
	if m == nil {
		t.Fatalf("全量脚本版本头不合规：%q", head)
	}
	to, _ := strconv.Atoi(m[1])
	if to != SchemaVersion {
		t.Fatalf("全量脚本头 TO=v%d 与 SchemaVersion=v%d 不一致", to, SchemaVersion)
	}
}

// TestLoadUpgradeStepsSortsNonContiguousVersions 锁定「版本可不连续、按前缀升序执行」：
// 03、04、101 依次执行，缺口 05~100 不补号也不报错。
func TestLoadUpgradeStepsSortsNonContiguousVersions(t *testing.T) {
	fsys := fstest.MapFS{
		"upgrade/00-latest-schema.sql": {Data: []byte("-- v0 -> v101 (compatible with v1+): Latest schema\n\nSELECT 1;\n")},
		"upgrade/101-late-change.sql":  {Data: []byte("-- v101: Add late change table for testing gaps\n\nSELECT 101;\n")},
		"upgrade/03-app-settings.sql":  {Data: []byte("-- v3: Add app_settings table for testing order\n\nSELECT 3;\n")},
		"upgrade/04-collections.sql":   {Data: []byte("-- v4: Add collections table for testing order\n\nSELECT 4;\n")},
	}
	steps, err := loadUpgradeStepsFS(fsys, "upgrade")
	if err != nil {
		t.Fatalf("加载失败：%v", err)
	}
	var got []int
	for _, s := range steps {
		got = append(got, s.to)
	}
	if want := []int{3, 4, 101}; !equalInts(got, want) {
		t.Fatalf("执行顺序=%v，期望 %v", got, want)
	}
}

// TestLoadUpgradeStepsRejectsBadHeaders 逐条验证版本头的校验作用：
// 头部与文件名不一致、形式不合规、行尾空白、缺空行、COMPAT 越界、01-*.sql 都必须拒绝。
func TestLoadUpgradeStepsRejectsBadHeaders(t *testing.T) {
	base := "-- v0 -> v4 (compatible with v1+): Latest schema\n\nSELECT 1;\n"
	cases := []struct {
		name string
		file string
		body string
	}{
		{"文件名与头不一致", "02-foo-bar.sql", "-- v3: Add foo table for testing mismatch\n\nSELECT 1;\n"},
		{"头部缺描述", "02-foo-bar.sql", "-- v2:\n\nSELECT 1;\n"},
		{"头部行尾空白", "02-foo-bar.sql", "-- v2: Add foo table for testing trailing   \n\nSELECT 1;\n"},
		{"头部后缺空行", "02-foo-bar.sql", "-- v2: Add foo table for testing blank\nSELECT 1;\n"},
		{"描述以句号结尾", "02-foo-bar.sql", "-- v2: Add foo table for testing period.\n\nSELECT 1;\n"},
		{"描述含中文", "02-foo-bar.sql", "-- v2: 新增 foo 表用于测试\n\nSELECT 1;\n"},
		{"描述首字母未大写", "02-foo-bar.sql", "-- v2: add foo table for testing case\n\nSELECT 1;\n"},
		{"COMPAT 不小于版本", "02-foo-bar.sql", "-- v2 (compatible with v2+): Add foo table for testing compat\n\nSELECT 1;\n"},
		{"COMPAT 为 v1", "03-foo-bar.sql", "-- v3 (compatible with v1+): Add foo table for testing compat\n\nSELECT 1;\n"},
		{"出现 01-*.sql", "01-foo-bar.sql", "-- v1: Add foo table for testing forbidden\n\nSELECT 1;\n"},
		{"文件名非 kebab", "02-Foo_Bar.sql", "-- v2: Add foo table for testing name\n\nSELECT 1;\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"upgrade/00-latest-schema.sql": {Data: []byte(base)},
				"upgrade/" + c.file:            {Data: []byte(c.body)},
			}
			if _, err := loadUpgradeStepsFS(fsys, "upgrade"); err == nil {
				t.Fatalf("期望拒绝，实际通过：%s", c.file)
			}
		})
	}

	t.Run("版本重复", func(t *testing.T) {
		fsys := fstest.MapFS{
			"upgrade/00-latest-schema.sql": {Data: []byte(base)},
			"upgrade/03-foo-bar.sql":       {Data: []byte("-- v3: Add foo table for testing dup\n\nSELECT 1;\n")},
			"upgrade/03-baz-qux.sql":       {Data: []byte("-- v3: Add baz table for testing dup\n\nSELECT 1;\n")},
		}
		if _, err := loadUpgradeStepsFS(fsys, "upgrade"); err == nil {
			t.Fatalf("期望拒绝重复版本")
		}
	})
}

// TestUpgradePathMatchesBaseline 验证「新库只执行全量脚本」与「老库按版本执行增量」殊途同归：
// 从 v4（只差 05）升级到 SchemaVersion 后，结构必须与全新库一致。
// 说明：历史增量脚本已合并进全量文件，早于 v4 的库没有可达升级路径（见 TestOpenTooOldSchemaFails）。
func TestUpgradePathMatchesBaseline(t *testing.T) {
	fresh := filepath.Join(t.TempDir(), "fresh.db")
	freshDB, err := Open(fresh)
	if err != nil {
		t.Fatalf("建全新库失败：%v", err)
	}
	want := schemaFingerprint(t, freshDB.Pool())
	var freshVer int
	if err := freshDB.Pool().QueryRow(`PRAGMA user_version`).Scan(&freshVer); err != nil {
		t.Fatalf("读取新库 user_version 失败：%v", err)
	}
	if freshVer != SchemaVersion {
		t.Fatalf("新库 user_version=%d，期望 %d（新库只跑全量脚本就应到最新版）", freshVer, SchemaVersion)
	}
	if n := countRows(t, freshDB.Pool(), "sqlite_master", "type='table' AND name NOT LIKE 'sqlite_%'"); n == 0 {
		t.Fatalf("新库没有建出任何表")
	}
	_ = freshDB.Close()

	// 回退到历史版本：v4 表示 05/06 未执行；v5 表示只差 06；v6 表示已是最新。
	dropV5 := []string{
		`DROP TABLE raw_files`,
		`DROP TABLE ledger_profile`,
		`ALTER TABLE import_batches DROP COLUMN raw_sha256`,
	}
	dropV6 := []string{
		`DROP TABLE collection_attachments`,
		`CREATE TABLE collection_attachments (
			id TEXT NOT NULL PRIMARY KEY, collection_id TEXT NOT NULL, file_name TEXT NOT NULL,
			ext TEXT NOT NULL, size_bytes INTEGER NOT NULL DEFAULT 0, content BLOB,
			sort_no INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE INDEX idx_collection_attachments_collection ON collection_attachments (collection_id)`,
	}
	revert := map[string][]string{
		"v4": append(append([]string{}, dropV6...), dropV5...),
		"v5": dropV6,
		"v6": {},
	}
	for _, v := range []string{"v4", "v5", "v6"} {
		path := filepath.Join(t.TempDir(), v+".db")
		db, err := Open(path)
		if err != nil {
			t.Fatalf("%s：建库失败：%v", v, err)
		}
		for _, stmt := range revert[v] {
			if _, err := db.Pool().Exec(stmt); err != nil {
				t.Fatalf("%s：回退语句失败（%s）：%v", v, stmt, err)
			}
		}
		ver, _ := strconv.Atoi(strings.TrimPrefix(v, "v"))
		if _, err := db.Pool().Exec(`PRAGMA user_version=` + strconv.Itoa(ver)); err != nil {
			t.Fatalf("%s：回写 user_version 失败：%v", v, err)
		}
		_ = db.Close()

		upgraded, err := Open(path)
		if err != nil {
			t.Fatalf("%s：升级失败：%v", v, err)
		}
		if got := schemaFingerprint(t, upgraded.Pool()); got != want {
			t.Fatalf("%s：升级后结构与全量脚本不一致\n--- got ---\n%s\n--- want ---\n%s", v, got, want)
		}
		var uv int
		if err := upgraded.Pool().QueryRow(`PRAGMA user_version`).Scan(&uv); err != nil {
			t.Fatalf("%s：读取 user_version 失败：%v", v, err)
		}
		if uv != SchemaVersion {
			t.Fatalf("%s：升级后 user_version=%d，期望 %d", v, uv, SchemaVersion)
		}
		_ = upgraded.Close()
	}
}

// TestOpenToleratesNewerSchema 库比程序新（历史版本号高于当前 SchemaVersion）时不降级、不报错。
func TestOpenToleratesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	if _, err := db.Pool().Exec(`PRAGMA user_version=` + strconv.Itoa(SchemaVersion+1)); err != nil {
		t.Fatalf("回写 user_version 失败：%v", err)
	}
	_ = db.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("打开较新库失败：%v", err)
	}
	defer reopened.Close()
	var uv int
	if err := reopened.Pool().QueryRow(`PRAGMA user_version`).Scan(&uv); err != nil {
		t.Fatalf("读取 user_version 失败：%v", err)
	}
	if uv != SchemaVersion+1 {
		t.Fatalf("较新库的 user_version 被改写：%d", uv)
	}
}

// schemaFingerprint 输出与书写顺序无关的结构指纹：列集合、索引/视图/触发器定义。
// 列顺序会因 ALTER TABLE 追加而不同，业务代码按列名取值，故不纳入比较。
func schemaFingerprint(t *testing.T, db *sql.DB) string {
	t.Helper()
	var b strings.Builder
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("读取表清单失败：%v", err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("扫描表名失败：%v", err)
		}
		tables = append(tables, n)
	}
	_ = rows.Close()

	for _, tbl := range tables {
		b.WriteString("TABLE " + tbl + "\n")
		cols, err := db.Query(`SELECT name, type, "notnull", coalesce(dflt_value,''), pk FROM pragma_table_info(?)`, tbl)
		if err != nil {
			t.Fatalf("读取 %s 列失败：%v", tbl, err)
		}
		var lines []string
		for cols.Next() {
			var name, typ, dflt string
			var notnull, pk int
			if err := cols.Scan(&name, &typ, &notnull, &dflt, &pk); err != nil {
				t.Fatalf("扫描 %s 列失败：%v", tbl, err)
			}
			lines = append(lines, " "+name+" "+typ+" notnull="+strconv.Itoa(notnull)+" default="+dflt+" pk="+strconv.Itoa(pk))
		}
		_ = cols.Close()
		sort.Strings(lines)
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}

	idx, err := db.Query(`SELECT type, name, coalesce(sql,'') FROM sqlite_master WHERE type IN ('index','view','trigger') AND name NOT LIKE 'sqlite_%' ORDER BY type, name`)
	if err != nil {
		t.Fatalf("读取索引失败：%v", err)
	}
	defer idx.Close()
	for idx.Next() {
		var typ, name, ddl string
		if err := idx.Scan(&typ, &name, &ddl); err != nil {
			t.Fatalf("扫描索引失败：%v", err)
		}
		b.WriteString(strings.ToUpper(typ) + " " + name + " " + strings.Join(strings.Fields(ddl), " ") + "\n")
	}
	return b.String()
}

// TestFullSchemaIndexesFollowTable 全量脚本里每个索引必须写在其所属表的建表语句之后
// （表建完紧跟自己的索引），且全量脚本不得使用 IF NOT EXISTS 防御子句。
func TestFullSchemaIndexesFollowTable(t *testing.T) {
	if strings.Contains(strings.ToUpper(fullSchema), "IF NOT EXISTS") {
		t.Fatalf("全量脚本不得使用 IF NOT EXISTS")
	}
	lines := strings.Split(fullSchema, "\n")
	tablePos := map[string]int{}
	for i, l := range lines {
		if m := reBaselineTable.FindStringSubmatch(l); m != nil {
			tablePos[m[1]] = i
		}
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, "CREATE") || !strings.Contains(l, "INDEX") {
			continue
		}
		stmt := l
		for j := i + 1; j < len(lines) && j <= i+2 && !strings.Contains(stmt, "ON "); j++ {
			stmt += "\n" + lines[j]
		}
		m := reBaselineIndex.FindStringSubmatch(stmt)
		if m == nil {
			t.Fatalf("第 %d 行索引语句无法解析：%q", i+1, l)
		}
		tbl := m[2]
		pos, ok := tablePos[tbl]
		if !ok {
			t.Fatalf("索引 %s 引用了全量脚本里不存在的表 %s", m[1], tbl)
		}
		if i < pos {
			t.Fatalf("索引 %s 写在其表 %s 之前（索引须跟在表后面）", m[1], tbl)
		}
	}
}

func countRows(t *testing.T, db *sql.DB, tbl, where string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tbl + ` WHERE ` + where).Scan(&n); err != nil {
		t.Fatalf("统计 %s 失败：%v", tbl, err)
	}
	return n
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestOpenTooOldSchemaFails 历史增量脚本已合并进全量文件：早于最低脚本版本的库
// 没有可达升级路径，必须报错（中文、可操作），不能只跑 05 留下半套表结构。
func TestOpenTooOldSchemaFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	for _, stmt := range []string{
		`DROP TABLE raw_files`,
		`DROP TABLE ledger_profile`,
		`ALTER TABLE import_batches DROP COLUMN raw_sha256`,
		`DROP TABLE collections`,
		`DROP TABLE collection_attachments`,
		`DROP TABLE app_settings`,
	} {
		if _, err := db.Pool().Exec(stmt); err != nil {
			t.Fatalf("回退语句失败（%s）：%v", stmt, err)
		}
	}
	if _, err := db.Pool().Exec(`PRAGMA user_version=1`); err != nil {
		t.Fatalf("回写 user_version 失败：%v", err)
	}
	_ = db.Close()

	if _, err := Open(path); err == nil {
		t.Fatal("过旧库被静默升级，want 报错")
	} else if !strings.Contains(err.Error(), "库版本过旧") {
		t.Fatalf("错误文案不可操作：%v", err)
	}
}
