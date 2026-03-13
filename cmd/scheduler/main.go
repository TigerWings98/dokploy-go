// Input: 环境变量配置, PostgreSQL, Redis
// Output: Cron 定时任务服务
// Role: 定时任务服务入口，管理备份/清理/自定义脚本等周期性任务
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // 嵌入时区数据库，确保精简容器中也能解析时区

	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/notify"
	"github.com/dokploy/dokploy/internal/scheduler"
)

func main() {
	cfg := config.Load()

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()

	notifier := notify.NewNotifier(database)
	sched := scheduler.New(database, cfg, notifier)
	sched.InitSchedules()

	log.Println("Dokploy scheduler started")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down scheduler...")
	sched.Stop()
	log.Println("Scheduler stopped")
}
