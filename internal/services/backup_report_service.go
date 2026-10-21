package services

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"finlite/internal/decimal"
	"finlite/internal/store"

	sdec "github.com/shopspring/decimal"
	_ "modernc.org/sqlite"
)

// BackupService 备份/恢复：文件级拷贝账套内的 finlite.db（流式拷贝，不整库进内存）。
// 备份文件落在 <账套目录>/backups/，文件名带账套码，恢复前校验账套身份。
// 恢复需二次确认 + 重启（前端 confirm 对话框保证）。
type BackupService struct {
	DB *store.DB
}

func NewBackupService(db *store.DB) *BackupService { return &BackupService{DB: db} }

// backupDir 备份目录：账套内 backups/（账套边界即备份边界）。
func backupDir(db *store.DB) string {
	return db.Ledger().BackupDir()
}

// Backup 立即备份，返回备份文件路径（文件名带账套码：ledger-1001_20260101_120000.db）。
func (s *BackupService) Backup(ctx context.Context) (string, error) {
	src := s.DB.Path()
	// WAL checkpoint 后再拷贝，保证一致性
	if err := s.DB.Checkpoint(); err != nil {
		return "", err
	}
	dir := backupDir(s.DB)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败：%v", err)
	}
	dst := filepath.Join(dir, s.DB.Ledger().Code+"_"+time.Now().Format("20060102_150405")+".db")
	if err := copyFile(src, dst); err != nil {
		return "", fmt.Errorf("备份失败：%v", err)
	}
	return dst, nil
}

// ListBackups 备份文件列表（倒序，含大小与时间：大库也能看出备份成本）。
func (s *BackupService) ListBackups(ctx context.Context) ([]BackupFile, error) {
	dir := backupDir(s.DB)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupFile{}, nil
		}
		return nil, fmt.Errorf("读取备份目录失败：%v", err)
	}
	out := make([]BackupFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		out = append(out, BackupFile{
			Name:      e.Name(),
			Path:      filepath.Join(dir, e.Name()),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// copyFile 流式拷贝（io.Copy + Sync）：百 GB 级库也不会把整库读进内存。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	if got, err := os.Stat(dst); err != nil || got.Size() != st.Size() {
		os.Remove(dst)
		return fmt.Errorf("备份文件大小不符（期望 %d 字节）", st.Size())
	}
	return nil
}

// Restore 用备份覆盖当前库（§7 F8「备份文件可恢复打开」）。
// 顺序：校验备份可打开 → checkpoint 当前库 → 留 pre-restore 快照 → 覆盖文件。
// 覆盖后当前连接持有的仍是旧文件句柄，必须重启应用才生效（前端二次确认后调 Restart）。
func (s *BackupService) Restore(ctx context.Context, backupPath string) (string, error) {
	if strings.TrimSpace(backupPath) == "" {
		return "", fmt.Errorf("备份文件不能为空")
	}
	if filepath.Ext(backupPath) != ".db" {
		return "", fmt.Errorf("只支持恢复 .db 备份文件：%s", filepath.Base(backupPath))
	}
	if _, err := os.Stat(backupPath); err != nil {
		return "", fmt.Errorf("备份文件不存在：%s", filepath.Base(backupPath))
	}
	// 1) 先验证备份确实是可打开的 SQLite 库（含核心表），避免用坏文件覆盖现网
	if err := verifyBackupOpenable(backupPath); err != nil {
		return "", err
	}
	// 2) 账套身份互校：禁止把别的账套的备份恢复进本账套
	if err := verifyBackupLedger(backupPath, s.DB.Ledger().Code); err != nil {
		return "", err
	}
	// 3) 当前库 checkpoint，保证 WAL 内容已落盘
	if err := s.DB.Checkpoint(); err != nil {
		return "", err
	}
	cur := s.DB.Path()
	// 4) 覆盖前留一份当前库快照，误恢复可回退
	dir := backupDir(s.DB)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败：%v", err)
	}
	safety := filepath.Join(dir, "pre-restore_"+s.DB.Ledger().Code+"_"+time.Now().Format("20060102_150405")+".db")
	if err := copyFile(cur, safety); err != nil {
		return "", fmt.Errorf("留存当前库快照失败：%v", err)
	}
	// 5) 覆盖主库文件，并清掉旧 WAL/SHM，避免与旧内容混合
	if err := copyFile(backupPath, cur); err != nil {
		return "", fmt.Errorf("恢复备份失败：%v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(cur + suffix); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("清理 %s 失败：%v", filepath.Base(cur+suffix), err)
		}
	}
	return safety, nil
}

// verifyBackupOpenable 打开备份并确认核心表存在（只读校验，不修改备份文件）。
func verifyBackupOpenable(backupPath string) error {
	conn, err := sql.Open("sqlite", backupPath)
	if err != nil {
		return fmt.Errorf("备份文件无法打开：%v", err)
	}
	defer conn.Close()
	for _, table := range []string{"bank_flows", "order_flows", "data_summaries", "vouchers"} {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			return fmt.Errorf("备份文件缺少表 %s，可能不是 finlite 备份：%v", table, err)
		}
	}
	return nil
}

// verifyBackupLedger 备份文件的账套码必须与当前账套一致，防止串档恢复。
func verifyBackupLedger(backupPath, wantCode string) error {
	conn, err := sql.Open("sqlite", backupPath)
	if err != nil {
		return fmt.Errorf("备份文件无法打开：%v", err)
	}
	defer conn.Close()
	var code string
	err = conn.QueryRow(`SELECT code FROM ledger_profile WHERE id=1`).Scan(&code)
	if err == sql.ErrNoRows || code == "" {
		// 旧版备份没有账套表：允许恢复，由开库时的身份校验兜底。
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取备份账套失败：%v", err)
	}
	if code != wantCode {
		return fmt.Errorf("备份属于账套 %s，当前账套 %s，已拒绝恢复", code, wantCode)
	}
	return nil
}

// ReportService 报表：科目余额表 lite + 收支日报。
type ReportService struct {
	DB *store.DB
}

func NewReportService(db *store.DB) *ReportService { return &ReportService{DB: db} }

// accountDirection 科目余额方向（§6.6）：资产/成本/费用在借方，收入/负债在贷方。
// 期末 = 期初 + 借方发生 - 贷方发生（借方科目）；贷方科目取相反方向。
func accountDirection(accountType string) int {
	switch accountType {
	case "asset", "cost", "expense":
		return 1 // 借方增加
	default:
		return -1 // income / liability：贷方增加
	}
}

// BalanceSheet 科目余额表：期初 + 本期借贷 = 期末，按期间筛（§7 F7）。
// 金额全程用 decimal：逐行读出 TEXT 后在 Go 里精确累加，不经 SQLite 的 SUM 数值强转
// （SUM(TEXT) 会被强转成 REAL，引入 float 误差，违反「金额禁用 float64」）。
func (s *ReportService) BalanceSheet(ctx context.Context, dateFrom, dateTo string) ([]BalanceRow, error) {
	// 不在此处按日期过滤：期初需要「期间开始日之前」的行，必须全部取回后在 Go 里分期。
	q := `SELECT ve.account_code, ve.account_name, v.voucher_date, ve.debit, ve.credit
		FROM voucher_entries ve JOIN vouchers v ON v.id=ve.voucher_id
		WHERE v.status='audited'
		ORDER BY ve.account_code, v.voucher_date`

	// 连接池 MaxOpenConns(1)：先把科目元数据读出来，再开游标，否则嵌套查询会死锁。
	types := s.accountTypes(ctx)
	canonical := s.accountNames(ctx)

	rows, err := s.DB.Pool().QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询余额表失败：%v", err)
	}
	defer rows.Close()

	type acc struct {
		name             string
		beginDr, beginCr sdec.Decimal
		curDr, curCr     sdec.Decimal
	}
	byCode := map[string]*acc{}
	for rows.Next() {
		var code, name, voucherDate, debit, credit string
		if err := rows.Scan(&code, &name, &voucherDate, &debit, &credit); err != nil {
			return nil, fmt.Errorf("读取余额表失败：%v", err)
		}
		d, err1 := parseZero(debit)
		c, err2 := parseZero(credit)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("余额表金额解析失败：%s", code)
		}
		a, ok := byCode[code]
		if !ok {
			// 展示名优先取科目表的标准名（同一科目代码可能有多个辅助核算名）。
			a = &acc{name: firstNonEmpty(canonical[code], name)}
			byCode[code] = a
		}
		switch {
		case dateFrom != "" && voucherDate < dateFrom:
			a.beginDr = a.beginDr.Add(d)
			a.beginCr = a.beginCr.Add(c)
		case dateTo != "" && voucherDate > dateTo:
			// 超出期间上界：不计入本期
		default:
			a.curDr = a.curDr.Add(d)
			a.curCr = a.curCr.Add(c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取余额表失败：%v", err)
	}

	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	out := make([]BalanceRow, 0, len(codes))
	for _, code := range codes {
		a := byCode[code]
		var begin, end sdec.Decimal
		if accountDirection(types[code]) == 1 {
			begin = a.beginDr.Sub(a.beginCr)
			end = begin.Add(a.curDr).Sub(a.curCr)
		} else {
			begin = a.beginCr.Sub(a.beginDr)
			end = begin.Add(a.curCr).Sub(a.curDr)
		}
		out = append(out, BalanceRow{
			AccountCode: code, AccountName: a.name,
			Begin: decimal.Format(begin), Debit: decimal.Format(a.curDr),
			Credit: decimal.Format(a.curCr), End: decimal.Format(end),
		})
	}
	return out, nil
}

// accountNames 科目代码 → 标准科目名（余额表展示用）。
func (s *ReportService) accountNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	rows, err := s.DB.Pool().QueryContext(ctx, `SELECT code, name FROM accounts`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var code, name string
		if err := rows.Scan(&code, &name); err == nil {
			out[code] = name
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// accountTypes 科目代码 → 科目类型（决定余额方向）。
func (s *ReportService) accountTypes(ctx context.Context) map[string]string {
	out := map[string]string{}
	rows, err := s.DB.Pool().QueryContext(ctx, `SELECT code, account_type FROM accounts`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var code, typ string
		if err := rows.Scan(&code, &typ); err == nil {
			out[code] = typ
		}
	}
	return out
}

func parseZero(s string) (sdec.Decimal, error) {
	if strings.TrimSpace(s) == "" {
		return sdec.Zero, nil
	}
	return decimal.Parse(s)
}

// DailyIncome 收支日报：按日汇总收入/支出（汇总表口径），decimal 精确累加。
func (s *ReportService) DailyIncome(ctx context.Context, dateFrom, dateTo string) ([]DailyRow, error) {
	q := `SELECT date, income_amount, expense_amount FROM data_summaries WHERE 1=1`
	var args []any
	if dateFrom != "" {
		q += ` AND date>=?`
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		q += ` AND date<=?`
		args = append(args, dateTo)
	}
	q += ` ORDER BY date`
	rows, err := s.DB.Pool().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询收支日报失败：%v", err)
	}
	defer rows.Close()
	type acc struct{ in, out sdec.Decimal }
	byDate := map[string]*acc{}
	var order []string
	for rows.Next() {
		var date, in, outAmt string
		if err := rows.Scan(&date, &in, &outAmt); err != nil {
			return nil, fmt.Errorf("读取收支日报失败：%v", err)
		}
		d, ok := byDate[date]
		if !ok {
			d = &acc{}
			byDate[date] = d
			order = append(order, date)
		}
		di, err1 := parseZero(in)
		do, err2 := parseZero(outAmt)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("日报金额解析失败：%s", date)
		}
		d.in = d.in.Add(di)
		d.out = d.out.Add(do)
	}
	var out []DailyRow
	for _, date := range order {
		d := byDate[date]
		out = append(out, DailyRow{Date: date, Income: decimal.Format(d.in), Expense: decimal.Format(d.out)})
	}
	if out == nil {
		out = []DailyRow{}
	}
	return out, rows.Err()
}
