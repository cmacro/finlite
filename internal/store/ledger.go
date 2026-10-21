// Package store 的账套（ledger）目录约定。
//
// MVP 单账套：一个运行实例只服务一个账套目录，结构固定为
//
//	<data root>/ledgers/ledger-1001/
//	    finlite.db                处理层（可重建派生 + 人工态）
//	    ledger.json               账套身份文件（code / display_name / created_at）
//	    raw/inbox/                待导入投放区
//	    raw/archive/YYYY/MM/      原始文件，只增不改
//	    attachments/YYYY/MM/      店面收款照片
//	    backups/                  全量备份
//
// 扩展路径：多账套只是多几个 ledgers/<code>/ 目录，ResolveLedger 换 code 即可，
// Service / 表结构 / 前端调用都不需要改——它们只拿到 *store.DB，路径统一走 Ledger。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultLedgerCode MVP 唯一账套码。多账套时这里只是默认值。
const DefaultLedgerCode = "ledger-1001"

// ledgerCodePattern 账套码约束：ASCII、短、可安全用于目录名与压缩包名。
var ledgerCodePattern = regexp.MustCompile(`^ledger-[a-z0-9]{1,16}$`)

// Ledger 账套目录：Code 是账套码，Root 是账套根目录（绝对路径）。
type Ledger struct {
	Code string
	Root string
}

// LedgerIdentity 账套身份（落盘为 ledger.json，与库内 ledger_profile 互校）。
type LedgerIdentity struct {
	Code        string `json:"code"`
	DisplayName string `json:"displayName"`
	LedgerDir   string `json:"ledgerDir"`
	CreatedAt   string `json:"createdAt"`
}

// DataRoot 数据根目录：FINLITE_DATA 优先，否则可执行文件同目录（单机单账套，整目录可搬）。
func DataRoot() string {
	if p := os.Getenv("FINLITE_DATA"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// DefaultLedger 默认账套：<data root>/ledgers/ledger-1001。
func DefaultLedger() Ledger {
	root := filepath.Join(DataRoot(), "ledgers", DefaultLedgerCode)
	return Ledger{Code: DefaultLedgerCode, Root: root}
}

// ResolveLedger 校验账套码并解析账套目录（不创建目录）。
func ResolveLedger(dataRoot, code string) (Ledger, error) {
	code = strings.TrimSpace(code)
	if !ledgerCodePattern.MatchString(code) {
		return Ledger{}, fmt.Errorf("账套码不合规（需形如 ledger-1001）：%s", code)
	}
	if strings.TrimSpace(dataRoot) == "" {
		return Ledger{}, fmt.Errorf("数据根目录为空，请设置 FINLITE_DATA 后重试")
	}
	abs, err := filepath.Abs(dataRoot)
	if err != nil {
		return Ledger{}, fmt.Errorf("数据根目录无效：%s", dataRoot)
	}
	return Ledger{Code: code, Root: filepath.Join(abs, "ledgers", code)}, nil
}

// DBPath 处理层库文件。
func (l Ledger) DBPath() string { return filepath.Join(l.Root, "finlite.db") }

// IdentityPath 账套身份文件 ledger.json。
func (l Ledger) IdentityPath() string { return filepath.Join(l.Root, "ledger.json") }

// RawInboxDir 待导入投放区（默认扫描目录）。
func (l Ledger) RawInboxDir() string { return filepath.Join(l.Root, "raw", "inbox") }

// RawArchiveDir 原始文件归档目录，按年月分桶。
func (l Ledger) RawArchiveDir(t time.Time) string {
	return filepath.Join(l.Root, "raw", "archive", t.Format("2006"), t.Format("01"))
}

// AttachDir 附件目录，按年月分桶。
func (l Ledger) AttachDir(t time.Time) string {
	return filepath.Join(l.Root, "attachments", t.Format("2006"), t.Format("01"))
}

// BackupDir 全量备份目录。
func (l Ledger) BackupDir() string { return filepath.Join(l.Root, "backups") }

// PackageDir 交接包输出目录（账套之外，便于整包外发）。
func (l Ledger) PackageDir() string {
	return filepath.Join(filepath.Dir(filepath.Dir(l.Root)), "packages")
}

// Rel 把账套内绝对路径转成相对账套根目录的斜杠路径（入库用，永不存绝对路径）。
func (l Ledger) Rel(abs string) (string, error) {
	rel, err := filepath.Rel(l.Root, abs)
	if err != nil {
		return "", fmt.Errorf("路径不在账套目录内：%s", abs)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("路径不在账套目录内：%s", abs)
	}
	return rel, nil
}

// Abs 把相对路径还原成账套内绝对路径，并拒绝越界（.. / 绝对路径）。
func (l Ledger) Abs(rel string) (string, error) {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" {
		return "", fmt.Errorf("相对路径为空")
	}
	if strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") ||
		strings.Contains(rel, "/../") {
		return "", fmt.Errorf("相对路径越界：%s", rel)
	}
	return filepath.Join(l.Root, filepath.FromSlash(rel)), nil
}

// Ensure 建齐账套目录结构（幂等）并补 ledger.json（已存在则不覆盖）。
func (l Ledger) Ensure() error {
	dirs := []string{
		l.Root,
		l.RawInboxDir(),
		filepath.Join(l.Root, "raw", "archive"),
		filepath.Join(l.Root, "attachments"),
		l.BackupDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("创建账套目录失败：%v", err)
		}
	}
	if _, err := os.Stat(l.IdentityPath()); err == nil {
		return nil
	}
	id := LedgerIdentity{
		Code:        l.Code,
		DisplayName: l.Code,
		LedgerDir:   l.Root,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("生成账套身份失败：%v", err)
	}
	if err := os.WriteFile(l.IdentityPath(), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("写入账套身份失败：%v", err)
	}
	return nil
}

// ReadIdentity 读取 ledger.json；不存在返回零值，不报错。
func (l Ledger) ReadIdentity() (LedgerIdentity, error) {
	data, err := os.ReadFile(l.IdentityPath())
	if err != nil {
		if os.IsNotExist(err) {
			return LedgerIdentity{}, nil
		}
		return LedgerIdentity{}, fmt.Errorf("读取账套身份失败：%v", err)
	}
	var id LedgerIdentity
	if err := json.Unmarshal(data, &id); err != nil {
		return LedgerIdentity{}, fmt.Errorf("账套身份文件损坏（%s）：%v", l.IdentityPath(), err)
	}
	return id, nil
}

// checkDirCode 账套目录名的账套码必须与期望一致（防把 A 账套目录改名成 B）。
func (l Ledger) checkDirCode() error {
	base := filepath.Base(l.Root)
	if base != l.Code {
		return fmt.Errorf("账套目录名与账套码不一致：目录 %s，账套 %s", base, l.Code)
	}
	return nil
}

// AdoptLegacy 把旧版布局（可执行文件同目录的 finlite.db / raw / backups）收进账套目录。
// 幂等：目标已存在则不动，返回本次搬进来的项目说明（空表示无需迁移）。
func (l Ledger) AdoptLegacy(dataRoot string) ([]string, error) {
	var adopted []string
	if _, err := os.Stat(l.DBPath()); os.IsNotExist(err) {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			src := filepath.Join(dataRoot, "finlite.db"+suffix)
			if _, serr := os.Stat(src); serr != nil {
				continue
			}
			if err := l.Ensure(); err != nil {
				return adopted, err
			}
			if err := movePathIfAbsent(src, l.DBPath()+suffix); err != nil {
				return adopted, err
			}
			if suffix == "" {
				adopted = append(adopted, "finlite.db")
			}
		}
	}
	for _, pair := range [][2]string{
		{filepath.Join(dataRoot, "raw"), filepath.Join(l.Root, "raw")},
		{filepath.Join(dataRoot, "backups"), l.BackupDir()},
	} {
		src, dst := pair[0], pair[1]
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := l.Ensure(); err != nil {
			return adopted, err
		}
		// 逐项并入：目标已存在的条目保留，源目录仅搬走未撞名的条目。
		entries, err := os.ReadDir(src)
		if err != nil {
			return adopted, fmt.Errorf("读取%s失败：%v", filepath.Base(src), err)
		}
		moved := false
		for _, e := range entries {
			s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
			if _, serr := os.Stat(d); serr == nil {
				continue
			}
			if err := movePathIfAbsent(s, d); err != nil {
				return adopted, err
			}
			moved = true
		}
		if moved {
			adopted = append(adopted, filepath.Base(src))
		}
		if rest, rerr := os.ReadDir(src); rerr == nil && len(rest) == 0 {
			_ = os.Remove(src)
		}
	}
	return adopted, nil
}

// movePathIfAbsent 目录/文件搬移：同盘 rename，跨盘回退为复制后删除。
func movePathIfAbsent(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败：%v", err)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	st, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("读取%s失败：%v", filepath.Base(src), err)
	}
	if st.IsDir() {
		return copyDir(src, dst)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取%s失败：%v", filepath.Base(src), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("写入%s失败：%v", filepath.Base(dst), err)
	}
	return os.Remove(src)
}

// copyDir 递归复制目录后删除源（跨盘搬移用）。
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("创建目录失败：%v", err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("读取目录失败：%v", err)
	}
	for _, e := range entries {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			return fmt.Errorf("读取%s失败：%v", e.Name(), err)
		}
		if err := os.WriteFile(d, data, 0o644); err != nil {
			return fmt.Errorf("写入%s失败：%v", e.Name(), err)
		}
	}
	return os.RemoveAll(src)
}
