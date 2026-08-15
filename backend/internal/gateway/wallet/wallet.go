// Package wallet 管理网关钱包、虚拟Key鉴权与计费扣款事务。
// 记账货币统一为 USD；扣费路径不做任何汇率换算，只在充值/价目表维护时换算。
package wallet

import (
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/askie/grix/backend/internal/gateway/virtualkey"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

var (
	// ErrKeyNotFound 表示传入的明文Key在系统里找不到匹配的哈希（key不存在或被篡改）。
	ErrKeyNotFound = errors.New("gateway: virtual key not found")
	// ErrKeyRevoked 表示Key存在但已被吊销。
	ErrKeyRevoked = errors.New("gateway: virtual key revoked")
	// ErrInsufficientBalance 表示钱包余额不足以放行本次请求。
	ErrInsufficientBalance = errors.New("gateway: insufficient balance")
)

type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// AuthResult 是虚拟Key鉴权通过后的上下文，供后续计费使用。
type AuthResult struct {
	Wallet     model.GatewayWallet
	VirtualKey model.GatewayVirtualKey
}

// CreateWallet 为一个 Grix 用户开一个网关钱包（幂等：已存在则直接返回）。
func (s *Service) CreateWallet(ownerID int64) (*model.GatewayWallet, error) {
	var wallet model.GatewayWallet
	err := s.db.Where("owner_id = ?", ownerID).First(&wallet).Error
	if err == nil {
		return &wallet, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	wallet = model.GatewayWallet{
		ID:      snowflake.GenID(),
		OwnerID: ownerID,
		Balance: decimal.Zero,
	}
	if err := s.db.Create(&wallet).Error; err != nil {
		return nil, err
	}
	return &wallet, nil
}

// IssueVirtualKey 给一个钱包开一把新的虚拟Key，返回明文（仅此一次）。
func (s *Service) IssueVirtualKey(walletID int64, label string) (plainKey string, key *model.GatewayVirtualKey, err error) {
	return s.IssueVirtualKeyForAgent(walletID, 0, label, "")
}

// IssueVirtualKeyForAgent 同 IssueVirtualKey，但把这把Key关联到指定的托管Agent
// （"Grix中转"C端场景用，agentID=0 等价于 IssueVirtualKey）。
// relayModel 是启用中转时为该Agent选定的服务端模型（可空），随Key落库供列表回显。
func (s *Service) IssueVirtualKeyForAgent(walletID, agentID int64, label, relayModel string) (plainKey string, key *model.GatewayVirtualKey, err error) {
	plain, hash, hint, err := virtualkey.Generate()
	if err != nil {
		return "", nil, fmt.Errorf("generate virtual key: %w", err)
	}
	record := model.GatewayVirtualKey{
		ID:         snowflake.GenID(),
		WalletID:   walletID,
		KeyHash:    hash,
		KeyHint:    hint,
		Label:      label,
		Status:     model.GatewayVirtualKeyStatusActive,
		AgentID:    agentID,
		RelayModel: relayModel,
	}
	if err := s.db.Create(&record).Error; err != nil {
		return "", nil, err
	}
	return plain, &record, nil
}

// RevokeVirtualKey 吊销一把Key，立即生效（不影响钱包下其它Key）。
func (s *Service) RevokeVirtualKey(keyID int64) error {
	return s.db.Model(&model.GatewayVirtualKey{}).
		Where("id = ? AND status = ?", keyID, model.GatewayVirtualKeyStatusActive).
		Updates(map[string]any{
			"status":     model.GatewayVirtualKeyStatusRevoked,
			"revoked_at": time.Now(),
		}).Error
}

// Authenticate 用明文虚拟Key换取钱包与Key记录；不查Grix登录态，只认这把Key本身。
func (s *Service) Authenticate(plainKey string) (*AuthResult, error) {
	hash := virtualkey.Hash(plainKey)

	var key model.GatewayVirtualKey
	if err := s.db.Where("key_hash = ?", hash).First(&key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	if key.Status != model.GatewayVirtualKeyStatusActive {
		return nil, ErrKeyRevoked
	}

	var wallet model.GatewayWallet
	if err := s.db.First(&wallet, key.WalletID).Error; err != nil {
		return nil, err
	}

	return &AuthResult{Wallet: wallet, VirtualKey: key}, nil
}

// TopUp 记一笔充值：source_amount(用户实际支付的币种/金额) 按 fxRateUsed 换算成USD记入钱包。
func (s *Service) TopUp(walletID int64, sourceCurrency string, sourceAmount, fxRateUsed decimal.Decimal, channel, reference string) (*model.GatewayWallet, error) {
	var wallet *model.GatewayWallet
	err := s.db.Transaction(func(tx *gorm.DB) error {
		w, _, e := creditTx(tx, walletID, sourceCurrency, sourceAmount, fxRateUsed, channel, reference)
		if e != nil {
			return e
		}
		wallet = w
		return nil
	})
	if err != nil {
		return nil, err
	}
	return wallet, nil
}

// creditTx 在给定事务内给钱包入账：锁钱包行、加余额、写一笔充值流水，返回更新后的钱包与流水。
// 供 TopUp（后台手工充值）与 CompleteTopup（支付成功自动入账）共用同一记账逻辑。
func creditTx(tx *gorm.DB, walletID int64, sourceCurrency string, sourceAmount, fxRateUsed decimal.Decimal, channel, reference string) (*model.GatewayWallet, *model.GatewayTopupRecord, error) {
	credited := sourceAmount.Mul(fxRateUsed)
	var wallet model.GatewayWallet
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&wallet, walletID).Error; err != nil {
		return nil, nil, err
	}
	wallet.Balance = wallet.Balance.Add(credited)
	if err := tx.Model(&wallet).Update("balance", wallet.Balance).Error; err != nil {
		return nil, nil, err
	}
	record := &model.GatewayTopupRecord{
		ID:               snowflake.GenID(),
		WalletID:         walletID,
		SourceCurrency:   sourceCurrency,
		SourceAmount:     sourceAmount,
		FxRateUsed:       fxRateUsed,
		CreditedAmount:   credited,
		PaymentChannel:   channel,
		PaymentReference: reference,
	}
	if err := tx.Create(record).Error; err != nil {
		return nil, nil, err
	}
	return &wallet, record, nil
}

// PreflightCheck 只做前置的粗粒度余额校验（余额>0才放行），不做预冻结。
func (s *Service) PreflightCheck(walletID int64) error {
	var wallet model.GatewayWallet
	if err := s.db.First(&wallet, walletID).Error; err != nil {
		return err
	}
	if wallet.Balance.LessThanOrEqual(decimal.Zero) {
		return ErrInsufficientBalance
	}
	return nil
}

// SettleRequest 是一次请求算完真实费用后要落的账。
type SettleRequest struct {
	WalletID         int64
	VirtualKeyID     int64
	RequestID        string
	Provider         string
	Model            string
	PromptTokens     int
	CachedTokens     int
	CompletionTokens int
	ReasoningTokens  int
	Cost             decimal.Decimal
}

// Settle 在一个事务里锁钱包行、扣款、写流水，返回扣款后的余额。
func (s *Service) Settle(req SettleRequest) (*model.GatewayLedgerEntry, error) {
	var entry model.GatewayLedgerEntry
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var wallet model.GatewayWallet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&wallet, req.WalletID).Error; err != nil {
			return err
		}
		wallet.Balance = wallet.Balance.Sub(req.Cost)
		if err := tx.Model(&wallet).Update("balance", wallet.Balance).Error; err != nil {
			return err
		}
		balanceAfter := wallet.Balance
		entry = model.GatewayLedgerEntry{
			ID:               snowflake.GenID(),
			WalletID:         req.WalletID,
			VirtualKeyID:     req.VirtualKeyID,
			RequestID:        req.RequestID,
			Provider:         req.Provider,
			Model:            req.Model,
			PromptTokens:     req.PromptTokens,
			CachedTokens:     req.CachedTokens,
			CompletionTokens: req.CompletionTokens,
			ReasoningTokens:  req.ReasoningTokens,
			Cost:             req.Cost,
			BalanceAfter:     &balanceAfter,
			Status:           model.GatewayLedgerStatusSettled,
		}
		return tx.Create(&entry).Error
	})
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// RecordFailure 记一笔未产生费用的失败流水（上游报错/中断，没拿到usage，不扣款）。
func (s *Service) RecordFailure(walletID, virtualKeyID int64, requestID, provider, model_ string) error {
	entry := model.GatewayLedgerEntry{
		ID:           snowflake.GenID(),
		WalletID:     walletID,
		VirtualKeyID: virtualKeyID,
		RequestID:    requestID,
		Provider:     provider,
		Model:        model_,
		Cost:         decimal.Zero,
		Status:       model.GatewayLedgerStatusFailed,
	}
	return s.db.Create(&entry).Error
}

// ListWallets 分页列出钱包，可按 owner_id 精确过滤（管理后台用）。
func (s *Service) ListWallets(ownerID int64, page, pageSize int) ([]model.GatewayWallet, int64, error) {
	q := s.db.Model(&model.GatewayWallet{})
	if ownerID > 0 {
		q = q.Where("owner_id = ?", ownerID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var wallets []model.GatewayWallet
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&wallets).Error; err != nil {
		return nil, 0, err
	}
	return wallets, total, nil
}

// GetWallet 按ID查单个钱包。
func (s *Service) GetWallet(id int64) (*model.GatewayWallet, error) {
	var w model.GatewayWallet
	if err := s.db.First(&w, id).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// GetVirtualKey 按ID查单把虚拟Key（不含明文，只有hash/hint），供调用方自行核对归属。
func (s *Service) GetVirtualKey(id int64) (*model.GatewayVirtualKey, error) {
	var key model.GatewayVirtualKey
	if err := s.db.First(&key, id).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// GetActiveVirtualKeyByAgent 查某个托管Agent名下当前有效（未吊销）的专属虚拟Key，
// 没有则返回 gorm.ErrRecordNotFound。用于"配置Agent用Grix中转"这个动作判断是否已经配过，
// 避免重复发一把新Key（虚拟Key明文只有创建那一刻能拿到，找不回旧的）。
func (s *Service) GetActiveVirtualKeyByAgent(walletID, agentID int64) (*model.GatewayVirtualKey, error) {
	var key model.GatewayVirtualKey
	err := s.db.Where("wallet_id = ? AND agent_id = ? AND status = ?", walletID, agentID, model.GatewayVirtualKeyStatusActive).
		First(&key).Error
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// ListVirtualKeys 列出一个钱包下的所有虚拟Key（不含明文，只有hash/hint）。
func (s *Service) ListVirtualKeys(walletID int64) ([]model.GatewayVirtualKey, error) {
	var keys []model.GatewayVirtualKey
	if err := s.db.Where("wallet_id = ?", walletID).Order("created_at DESC").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

// ListLedgerEntries 分页列出一个钱包的消费流水。
func (s *Service) ListLedgerEntries(walletID int64, page, pageSize int) ([]model.GatewayLedgerEntry, int64, error) {
	q := s.db.Model(&model.GatewayLedgerEntry{}).Where("wallet_id = ?", walletID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var entries []model.GatewayLedgerEntry
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// ListTopupRecords 分页列出一个钱包的充值流水。
func (s *Service) ListTopupRecords(walletID int64, page, pageSize int) ([]model.GatewayTopupRecord, int64, error) {
	q := s.db.Model(&model.GatewayTopupRecord{}).Where("wallet_id = ?", walletID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []model.GatewayTopupRecord
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}
