package services

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"finlite/internal/store"
)

// AccountService 科目维护（树形 code/name/parent_code/type）。
type AccountService struct {
	DB *store.DB
}

func NewAccountService(db *store.DB) *AccountService { return &AccountService{DB: db} }

// List 科目列表。
func (s *AccountService) List(ctx context.Context) ([]Account, error) {
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT code, name, COALESCE(parent_code,''), account_type, is_active
		 FROM accounts ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("查询科目失败：%v", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.Code, &a.Name, &a.ParentCode, &a.AccountType, &a.IsActive); err != nil {
			return nil, fmt.Errorf("读取科目失败：%v", err)
		}
		out = append(out, a)
	}
	if out == nil {
		out = []Account{}
	}
	return out, rows.Err()
}

// Upsert 新增或更新科目。
func (s *AccountService) Upsert(ctx context.Context, a Account) (Account, error) {
	if strings.TrimSpace(a.Code) == "" || strings.TrimSpace(a.Name) == "" {
		return Account{}, fmt.Errorf("科目代码和名称不能为空")
	}
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO accounts (code, name, parent_code, account_type, is_active)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(code) DO UPDATE SET name=excluded.name, parent_code=excluded.parent_code,
				account_type=excluded.account_type, is_active=excluded.is_active`,
			a.Code, a.Name, nullIfEmptyStr(a.ParentCode), a.AccountType, a.IsActive)
		return err
	})
	if err != nil {
		return Account{}, fmt.Errorf("保存科目失败：%v", err)
	}
	return a, nil
}

// FeeRateService 费率维护。
type FeeRateService struct {
	DB *store.DB
}

func NewFeeRateService(db *store.DB) *FeeRateService { return &FeeRateService{DB: db} }

// List 费率列表。
func (s *FeeRateService) List(ctx context.Context) ([]FeeRate, error) {
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT pay_method, rate, updated_at FROM fee_rates ORDER BY pay_method`)
	if err != nil {
		return nil, fmt.Errorf("查询费率失败：%v", err)
	}
	defer rows.Close()
	var out []FeeRate
	for rows.Next() {
		var r FeeRate
		if err := rows.Scan(&r.PayMethod, &r.Rate, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取费率失败：%v", err)
		}
		out = append(out, r)
	}
	if out == nil {
		out = []FeeRate{}
	}
	return out, rows.Err()
}

// Update 更新费率。
func (s *FeeRateService) Update(ctx context.Context, payMethod, rate string) (FeeRate, error) {
	if strings.TrimSpace(payMethod) == "" || strings.TrimSpace(rate) == "" {
		return FeeRate{}, fmt.Errorf("支付方式和费率不能为空")
	}
	now := time.Now().Format(time.RFC3339)
	if _, err := s.DB.Pool().ExecContext(ctx,
		`UPDATE fee_rates SET rate=?, updated_at=? WHERE pay_method=?`, rate, now, payMethod); err != nil {
		return FeeRate{}, fmt.Errorf("更新费率失败：%v", err)
	}
	return FeeRate{PayMethod: payMethod, Rate: rate, UpdatedAt: now}, nil
}

func nullIfEmptyStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
