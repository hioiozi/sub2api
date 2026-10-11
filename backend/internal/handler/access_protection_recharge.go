package handler

import "context"

// TotalRecharged is the read-only adapter for the access quota extension.
func (h *RedeemHandler) TotalRecharged(ctx context.Context, userID int64) (float64, error) {
	return h.redeemService.AccessProtectionTotalRecharged(ctx, userID)
}
