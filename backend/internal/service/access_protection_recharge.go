package service

import (
	"context"
	"errors"

	entsql "entgo.io/ent/dialect/sql"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
)

// AccessProtectionTotalRecharged adds administrator credits to the upstream
// cumulative field. Balance/redeem credits are already in that field; adding
// their history again would double count them. Negative adjustments never
// reduce this lifetime amount. This read does not mutate billing data.
func (s *RedeemService) AccessProtectionTotalRecharged(ctx context.Context, userID int64) (float64, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	if s.entClient == nil {
		return 0, errors.New("access recharge database unavailable")
	}
	var totals []struct {
		Sum float64 `json:"sum"`
	}
	err = s.entClient.RedeemCode.Query().Where(
		redeemcode.UsedByEQ(userID),
		redeemcode.TypeEQ(AdjustmentTypeAdminBalance),
		redeemcode.StatusEQ(StatusUsed),
		redeemcode.ValueGT(0),
	).Aggregate(dbent.As(func(selector *entsql.Selector) string {
		return "COALESCE(" + entsql.Sum(selector.C(redeemcode.FieldValue)) + ", 0)"
	}, "sum")).Scan(ctx, &totals)
	if err != nil {
		return 0, err
	}
	if len(totals) == 0 {
		return user.TotalRecharged, nil
	}
	return user.TotalRecharged + totals[0].Sum, nil
}
