package app

import (
	"embed"
	"log"

	"finlite/internal/services"
	"finlite/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Run 初始化应用：打开 finlite.db，注册全部 Service，建桌面窗口。
// assets 为 main.go 嵌入的 frontend/dist（go:embed 必须在拥有文件的目录）。
func Run(assets embed.FS) error {
	db, err := store.Open(store.DefaultPath())
	if err != nil {
		log.Printf("打开数据库失败：%v", err)
		return err
	}

	app := application.New(application.Options{
		Name:        "finlite",
		Description: "健康管理公司简易财务助手：导入→对账→汇总→凭证→余额表",
		Services: []application.Service{
			application.NewService(services.NewImportService(db)),
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
