package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"finlite/internal/decimal"
	"finlite/internal/store"
)

// BackupService 备份/恢复：文件级拷贝 finlite.db。
// 恢复需二次确认 + 重启（前端 confirm 对话框保证）。
type BackupService struct {
	DB *store.DB
}

func NewBackupService(db *store.DB) *BackupService { return &BackupService{DB: db} }

func backupDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "backups")
}

// Backup 立即备份，返回备份文件路径。
func (s *BackupService) Backup(ctx context.Context) (string, error) {
	src := s.DB.Path()
	// WAL checkpoint 后再拷贝，保证一致性
	if _, err := s.DB.Pool().ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return "", fmt.Errorf("检查点失败：%v", err)
	}
	dir := backupDir(src)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败：%v", err)
	}
	dst := filepath.Join(dir, "finlite_"+time.Now().Format("20060102_150405")+".db")
	if err := copyFile(src, dst); err != nil {
		return "", fmt.Errorf("备份失败：%v", err)
	}
	return dst, nil
}

// ListBackups 备份文件列表（倒序）。
func (s *BackupService) ListBackups(ctx context.Context) ([]string, error) {
	dir := backupDir(s.DB.Path())
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取备份目录失败：%v", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// ReportService 报表：科目余额表 lite + 收支日报。
type ReportService struct {
	DB *store.DB
}

func NewReportService(db *store.DB) *ReportService { return &ReportService{DB: db} }

// BalanceSheet 科目余额表：期末 = 期初 + 借 - 贷（按借方科目方向简化）。
// 期初暂取 0，本期发生取凭证分录汇总。
func (s *ReportService) BalanceSheet(ctx context.Context, dateFrom, dateTo string) ([]BalanceRow, error) {
	q := `SELECT ve.account_code, ve.account_name,
		COALESCE(SUM(CAST(ve.debit AS REAL)),0), COALESCE(SUM(CAST(ve.credit AS REAL)),0)
		FROM voucher_entries ve JOIN vouchers v ON v.id=ve.voucher_id WHERE 1=1`
	var args []any
	if dateFrom != "" {
		q += ` AND v.voucher_date>=?`
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		q += ` AND v.voucher_date<=?`
		args = append(args, dateTo)
	}
	q += ` GROUP BY ve.account_code, ve.account_name ORDER BY ve.account_code`
	rows, err := s.DB.Pool().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询余额表失败：%v", err)
	}
	defer rows.Close()
	var out []BalanceRow
	for rows.Next() {
		var code, name string
		var debit, credit float64
		if err := rows.Scan(&code, &name, &debit, &credit); err != nil {
			return nil, fmt.Errorf("读取余额表失败：%v", err)
		}
		// 金额回 decimal 格式化，避免 float 展示误差
		dStr := decimal.Format(decimal.MustParse(fmt.Sprintf("%v", debit)))
		cStr := decimal.Format(decimal.MustParse(fmt.Sprintf("%v", credit)))
		end := decimal.Format(decimal.MustParse(fmt.Sprintf("%v", debit-credit)))
		out = append(out, BalanceRow{
			AccountCode: code, AccountName: name,
			Begin: "0.00", Debit: dStr, Credit: cStr, End: end,
		})
	}
	if out == nil {
		out = []BalanceRow{}
	}
	return out, rows.Err()
}

// DailyIncome 收支日报：按日汇总收入/支出（汇总表口径）。
func (s *ReportService) DailyIncome(ctx context.Context, dateFrom, dateTo string) ([]DailyRow, error) {
	q := `SELECT date, COALESCE(SUM(CAST(income_amount AS REAL)),0), COALESCE(SUM(CAST(expense_amount AS REAL)),0)
		FROM data_summaries WHERE 1=1`
	var args []any
	if dateFrom != "" {
		q += ` AND date>=?`
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		q += ` AND date<=?`
		args = append(args, dateTo)
	}
	q += ` GROUP BY date ORDER BY date`
	rows, err := s.DB.Pool().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询收支日报失败：%v", err)
	}
	defer rows.Close()
	var out []DailyRow
	for rows.Next() {
		var date string
		var in, outAmt float64
		if err := rows.Scan(&date, &in, &outAmt); err != nil {
			return nil, fmt.Errorf("读取收支日报失败：%v", err)
		}
		out = append(out, DailyRow{Date: date, Income: fmt.Sprintf("%.2f", in), Expense: fmt.Sprintf("%.2f", outAmt)})
	}
	if out == nil {
		out = []DailyRow{}
	}
	return out, rows.Err()
}
