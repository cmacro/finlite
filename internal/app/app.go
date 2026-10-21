package app

import (
	"context"
	"embed"
	"log"
	"os"

	"finlite/internal/services"
	"finlite/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Run 初始化应用：打开账套（默认 <data root>/ledgers/ledger-1001），注册全部 Service，建桌面窗口。
// assets 为 main.go 嵌入的 frontend/dist（go:embed 必须在拥有文件的目录）。
func Run(assets embed.FS) error {
	db, err := openLedger()
	if err != nil {
		log.Printf("打开账套失败：%v", err)
		return err
	}

	// 存量原始文件迁移（幂等）：旧版 raw/ 收进账套目录并补登记指纹，失败不阻断启动。
	if report, merr := services.NewRawFileService(db).MigrateLegacy(context.Background()); merr != nil {
		log.Printf("存量原始文件迁移失败：%v", merr)
	} else if report.MovedInbox+report.MovedArchive+report.Registered+report.LinkedBatches > 0 {
		log.Printf("存量原始文件迁移：%s", report.Message)
	}

	app := application.New(application.Options{
		Name:        "finlite",
		Description: "健康管理公司简易财务助手：导入→对账→汇总→凭证→余额表",
		Services: []application.Service{
			application.NewService(services.NewImportService(db)),
			application.NewService(services.NewRawFileService(db)),
			application.NewService(services.NewBankFlowService(db)),
			application.NewService(services.NewOrderFlowService(db)),
			application.NewService(services.NewCollectionService(db)),
			application.NewService(services.NewReconService(db)),
			application.NewService(services.NewSummaryService(db)),
			application.NewService(services.NewVoucherService(db)),
			application.NewService(services.NewAccountService(db)),
			application.NewService(services.NewFeeRateService(db)),
			application.NewService(services.NewBackupService(db)),
			application.NewService(services.NewReportService(db)),
			application.NewService(services.NewAuditService(db)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "finlite 财务助手",
		Width:  1280,
		Height: 800,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		// 窗口底色：WebView 加载完成前的首屏颜色，需与 styles/tokens.css --color-surface 一致。
		BackgroundColour: application.NewRGB(255, 255, 255),
		URL:              "/",
	})

	return app.Run()
}

// openLedger 打开账套：FINLITE_DB 可指定单个库文件（调试/测试），否则用默认账套目录。
func openLedger() (*store.DB, error) {
	if p := os.Getenv("FINLITE_DB"); p != "" {
		return store.Open(p)
	}
	dataRoot := store.DataRoot()
	ledger := store.DefaultLedger()
	// 旧版把 finlite.db / raw / backups 放在可执行文件同目录：一次性收进账套目录。
	if adopted, err := ledger.AdoptLegacy(dataRoot); err != nil {
		log.Printf("旧版账套目录迁移失败：%v", err)
	} else if len(adopted) > 0 {
		log.Printf("已把旧版数据收进账套目录 %s：%v", ledger.Root, adopted)
	}
	return store.OpenLedger(ledger)
}
