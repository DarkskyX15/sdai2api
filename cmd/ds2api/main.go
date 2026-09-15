package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/server"
	"ds2api/internal/webui"
)

func main() {
	if err := config.LoadDotEnv(); err != nil {
		config.Logger.Warn("[dotenv] load failed", "error", err)
	}
	config.RefreshLogger()
	webui.EnsureBuiltOnStartup()
	_ = auth.AdminKey()
	app, err := server.NewApp()
	if err != nil {
		config.Logger.Error("server initialization failed", "error", err)
		os.Exit(1)
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "5001"
	}
	// DS2API_BIND 控制监听地址：默认 0.0.0.0（Docker/局域网部署兼容）。
	// 仅本机使用时建议显式设为 127.0.0.1，避免服务暴露到局域网。
	bind := strings.TrimSpace(os.Getenv("DS2API_BIND"))
	if bind == "" {
		bind = "0.0.0.0"
	}
	if strings.EqualFold(bind, "localhost") {
		bind = "127.0.0.1"
	}
	exposeAllInterfaces := bind == "0.0.0.0" || bind == "::"

	srv := &http.Server{
		Addr:              bind + ":" + port,
		Handler:           app.Router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	localURL := fmt.Sprintf("http://127.0.0.1:%s", port)
	lanIP := ""
	lanURL := ""
	if exposeAllInterfaces {
		lanIP = detectLANIPv4()
		if lanIP != "" {
			lanURL = fmt.Sprintf("http://%s:%s", lanIP, port)
		}
	}

	// Start server in a goroutine so we can listen for shutdown signals.
	go func() {
		if lanURL != "" {
			config.Logger.Info("starting ds2api", "bind", srv.Addr, "port", port, "local_url", localURL, "lan_url", lanURL, "lan_ip", lanIP)
		} else if exposeAllInterfaces {
			config.Logger.Info("starting ds2api", "bind", srv.Addr, "port", port, "local_url", localURL)
			config.Logger.Warn("lan ip not detected; check active network interfaces")
		} else {
			config.Logger.Info("starting ds2api", "bind", srv.Addr, "port", port, "local_url", localURL)
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			config.Logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal (Ctrl+C / SIGTERM).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	sig := <-quit
	config.Logger.Info("shutdown signal received", "signal", sig.String())

	// Graceful shutdown: allow up to 10 seconds for in-flight requests to complete.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		config.Logger.Error("graceful shutdown failed, forcing exit", "error", err)
		os.Exit(1)
	}
	config.Logger.Info("server gracefully stopped")
}

func detectLANIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}
			ip = ip.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}
