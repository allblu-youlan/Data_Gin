package bootstrap

// InitializeProviderCLI loads provider configuration and shared Redis clients
// without starting database migrations, workers, cron jobs, or schedulers.
func InitializeProviderCLI() {
	setupConfig()
	setupLogger()
	setupRedis()
}

// InitializeProviderDatabaseCLI adds the existing database connection needed by
// one-shot sync commands, without starting background jobs or AutoMigrate.
func InitializeProviderDatabaseCLI() {
	InitializeMigration()
	setupRedis()
}
