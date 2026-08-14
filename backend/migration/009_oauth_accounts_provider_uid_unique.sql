CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_accounts_provider_uid_unique
ON oauth_accounts(provider, provider_uid);
