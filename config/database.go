package config

import (
	"gin-biz-web-api/pkg/config"
)

func init() {
	config.Add("cfg.database", func() map[string]interface{} {
		return map[string]interface{}{

			// 默认数据库
			"driver":                       config.Get("DB.Driver", "mysql"),
			"migration_io_timeout_seconds": config.Get("DB.MigrationIOTimeoutSeconds", 30*60),

			"mysql": map[string]interface{}{
				"default": map[string]interface{}{
					// 数据库连接信息
					"host":               config.Get("DB.Host", "127.0.0.1"),
					"port":               config.Get("DB.Port", 3306),
					"database":           config.Get("DB.Database", "gin-biz-web-api"),
					"username":           config.Get("DB.Username"),
					"password":           config.Get("DB.Password"),
					"charset":            config.Get("DB.Charset", "utf8mb4"),
					"interpolate_params": databaseBool("DB.InterpolateParams", true),

					// 连接池配置
					"max_open_connections":    config.Get("DB.MaxOpenConnections", 25),
					"max_idle_connections":    config.Get("DB.MaxIdleConnections", 10),
					"max_life_seconds":        config.Get("DB.MaxLifeSeconds", 5*60),
					"max_idle_seconds":        config.Get("DB.MaxIdleSeconds", 90),
					"connect_timeout_seconds": config.Get("DB.ConnectTimeoutSeconds", 5),
					"read_timeout_seconds":    config.Get("DB.ReadTimeoutSeconds", 30),
					"write_timeout_seconds":   config.Get("DB.WriteTimeoutSeconds", 30),
					"reject_read_only":        databaseBool("DB.RejectReadOnly", true),
				},

				// 如果有多个数据库连接，可以模仿 default 配置信息再增加一个，比如：
				// "db1": map[string]interface{}{
				// 	// 数据库连接信息
				// 	"host":     config.Get("DB1.Host", "127.0.0.1"),
				// 	"port":     config.Get("DB1.Port", 3306),
				// 	"database": config.Get("DB1.Database", "gin-biz-web-api"),
				// 	"username": config.Get("DB1.Username"),
				// 	"password": config.Get("DB1.Password"),
				// 	"charset":  config.Get("DB1.Charset", "utf8mb4"),
				//
				// 	// 连接池配置
				// 	"max_open_connections": config.Get("DB1.MaxOpenConnections", 25),  // 最大连接数
				// 	"max_idle_connections": config.Get("DB1.MaxIdleConnections", 100), // 最大空闲连接数
				// 	"max_life_seconds":     config.Get("DB1.MaxLifeSeconds", 5*60),    // 每个链接的过期时间
				// },

			},
		}
	})
}

func databaseBool(path string, defaultValue bool) bool {
	instance := config.Instance()
	if instance == nil || !instance.IsSet(path) {
		return defaultValue
	}
	return instance.GetBool(path)
}
