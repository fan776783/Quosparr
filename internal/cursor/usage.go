package cursor

import (
	"math"
	"time"
)

// Pool contains only a server-reported usage pool, not inferred subscription prices.
type Pool struct {
	Kind                  string
	UsedPercent           *float64
	UsedCents, LimitCents *int64
	// LimitEstimated marks amounts inferred from spend and percentages.
	LimitEstimated bool
	ResetsAt       time.Time
	WindowMinutes  int
}

type Credit struct {
	Name           string
	RemainingCents int64
	ExpiresAt      time.Time
}

type Snapshot struct {
	Plan          string
	Pools         []Pool
	OnDemandCents *int64
	Credits       []Credit
	Shared        bool
	UpdatedAt     time.Time
}

// ParseUsage decodes the known fields of Cursor desktop's DashboardService.
// Non-optional proto3 scalars default to zero only inside a recognized message;
// optional fields retain absence and cannot manufacture an unused quota.
func ParseUsage(planBody, usageBody []byte, now time.Time) (Snapshot, error) {
	plan, err := parsePlan(planBody)
	if err != nil {
		return Snapshot{}, err
	}
	usage, err := readMessage(usageBody)
	if err != nil {
		return Snapshot{}, err
	}
	enabled, err := usage.integer(6)
	if err != nil {
		return Snapshot{}, err
	}
	if number(enabled) != 1 {
		return Snapshot{}, ErrUnavailable
	}
	out := Snapshot{Plan: plan.name, Shared: plan.shared, UpdatedAt: now}
	out.Pools, err = parseUsagePools(usage, plan)
	if err != nil {
		return Snapshot{}, err
	}
	out.OnDemandCents, err = parseAdditionalSpend(usage)
	if err != nil {
		return Snapshot{}, err
	}
	if len(out.Pools) == 0 && out.OnDemandCents == nil {
		return Snapshot{}, ErrResponse
	}
	return out, nil
}

type planInfo struct {
	name   string
	shared bool
	period int64
	reset  *int64
}

func parsePlan(body []byte) (planInfo, error) {
	var out planInfo
	plan, err := readMessage(body)
	if err != nil {
		return out, err
	}
	info, err := plan.nested(1)
	if err != nil {
		return out, err
	}
	out.name, err = info.text(1)
	if err != nil {
		return out, err
	}
	merged, err := plan.integer(3)
	if err != nil {
		return out, err
	}
	out.shared = number(merged) != 0
	period, err := info.integer(6)
	if err != nil {
		return out, err
	}
	out.period = number(period)
	out.reset, err = info.integer(7)
	return out, err
}

func parseUsagePools(usage message, plan planInfo) ([]Pool, error) {
	p, err := usage.nested(3)
	if err != nil || p == nil {
		return nil, err
	}
	pools, err := parseBuckets(p, plan.shared)
	if err != nil {
		return nil, err
	}
	reset := plan.reset
	// Only a monthly period shares the billing-cycle end; weekly resets are
	// independent and an unspecified period (zero) is not inferred.
	if reset == nil && plan.period == 1 {
		reset, err = usage.integer(2)
		if err != nil {
			return nil, err
		}
	}
	end, err := timestamp(reset)
	if err != nil {
		return nil, err
	}
	window := 0
	if plan.period == 1 {
		window = 30 * 24 * 60
	}
	if plan.period == 2 {
		window = 7 * 24 * 60
	}
	for i := range pools {
		pools[i].ResetsAt, pools[i].WindowMinutes = end, window
	}
	return pools, nil
}

// parseBuckets reads the percentages the usage page shows. A merged plan has
// one shared pool; otherwise "auto" (Cursor models) and "api" (other models)
// are separate. The combined percentage is used only to estimate amounts,
// never as an additional pool or as the paid included-spend limit.
func parseBuckets(p message, shared bool) ([]Pool, error) {
	if shared {
		pct, err := p.percent(14)
		if err != nil || pct == nil {
			return nil, err
		}
		return []Pool{{Kind: "shared", UsedPercent: pct}}, nil
	}
	var pools []Pool
	for _, spec := range []struct {
		name                 string
		used, limit, percent uint32
	}{{"auto", 8, 10, 12}, {"api", 9, 11, 13}} {
		pool, err := parsePool(p, spec.name, spec.used, spec.limit, spec.percent)
		if err != nil {
			return nil, err
		}
		if pool != nil {
			pools = append(pools, *pool)
		}
	}
	return pools, estimateBucketAmounts(p, pools)
}

// The combined percentage is weighted by the two pools' caps. Together with
// total spend it yields both caps, even when one pool has no usage events:
// totalCap = totalSpend / totalPercent, and
// autoCap / totalCap = (totalPercent - apiPercent) / (autoPercent - apiPercent).
// This uses one server snapshot, avoiding event/percentage timing differences.
// Equal percentages, capped percentages and an entirely unused cycle cannot
// determine the split. Explicit server amounts always take precedence.
func estimateBucketAmounts(m message, pools []Pool) error {
	if len(pools) != 2 || pools[0].UsedPercent == nil || pools[1].UsedPercent == nil {
		return nil
	}
	total, err := m.integer(1)
	if err != nil {
		return err
	}
	pct, err := m.percent(14)
	if err != nil {
		return err
	}
	if total == nil || *total <= 0 || pct == nil || *pct <= 0 || *pct >= maxInvertPercent {
		return nil
	}
	auto, api := *pools[0].UsedPercent, *pools[1].UsedPercent
	if auto >= maxInvertPercent || api >= maxInvertPercent || *pct <= min(auto, api) || *pct >= max(auto, api) {
		return nil
	}
	totalCap := float64(*total) / *pct * 100
	autoCap := totalCap * (*pct - api) / (auto - api)
	caps := [2]float64{autoCap, totalCap - autoCap}
	var used, limits [2]int64
	for i, cap := range caps {
		limit := math.Round(cap/1000) * 1000 // Match the existing $10 estimate precision.
		spend := math.Round(cap * *pools[i].UsedPercent / 100)
		if math.IsNaN(limit) || math.IsInf(limit, 0) || limit <= 0 || limit >= float64(math.MaxInt64) ||
			math.IsNaN(spend) || math.IsInf(spend, 0) || spend < 0 || spend >= float64(math.MaxInt64) {
			return nil
		}
		used[i], limits[i] = int64(spend), int64(limit)
		// Conflicting explicit amounts mean the weighted split is not usable.
		if p := pools[i]; p.LimitCents != nil && *p.LimitCents != limits[i] || p.UsedCents != nil && *p.UsedCents != used[i] {
			return nil
		}
	}
	for i := range pools {
		p := &pools[i]
		if p.UsedCents == nil && p.LimitCents == nil {
			p.UsedCents, p.LimitCents, p.LimitEstimated = &used[i], &limits[i], true
		}
	}
	return nil
}

func parseAdditionalSpend(usage message) (*int64, error) {
	spend, err := usage.nested(4)
	if err != nil || spend == nil {
		return nil, err
	}
	amount, err := spend.integer(1)
	if err != nil {
		return nil, err
	}
	// A present empty message is zero additional spend in the unary schema.
	if amount == nil {
		zero := int64(0)
		amount = &zero
	}
	return amount, nil
}

func parsePool(m message, name string, usedKey, limitKey, pctKey uint32) (*Pool, error) {
	p := Pool{Kind: name}
	var err error
	p.UsedCents, err = m.integer(usedKey)
	if err != nil {
		return nil, err
	}
	p.LimitCents, err = m.integer(limitKey)
	if err != nil {
		return nil, err
	}
	p.UsedPercent, err = m.percent(pctKey)
	if err != nil {
		return nil, err
	}
	if p.UsedPercent == nil && number(p.LimitCents) > 0 && p.UsedCents != nil {
		v := float64(*p.UsedCents) / float64(*p.LimitCents) * 100
		p.UsedPercent = &v
	}
	if p.UsedPercent == nil && p.UsedCents == nil && p.LimitCents == nil {
		return nil, nil
	}
	return &p, nil
}

func timestamp(n *int64) (time.Time, error) {
	if number(n) == 0 {
		return time.Time{}, nil
	}
	// Do not mistake seconds for the desktop's Unix milliseconds.
	if *n < 946684800000 || *n > 32503680000000 {
		return time.Time{}, ErrResponse
	}
	return time.UnixMilli(*n), nil
}

func ParseCredits(body []byte) ([]Credit, error) {
	m, err := readMessage(body)
	if err != nil {
		return nil, err
	}
	var out []Credit
	for _, v := range m[1] {
		if v.kind != 2 || len(out) >= 64 {
			return nil, ErrResponse
		}
		g, err := readMessage(v.data)
		if err != nil {
			return nil, err
		}
		remaining, err := g.integer(1)
		if err != nil {
			return nil, err
		}
		name, err := g.text(4)
		if err != nil {
			return nil, err
		}
		expires, err := g.integer(3)
		if err != nil {
			return nil, err
		}
		at, err := timestamp(expires)
		if err != nil {
			return nil, err
		}
		out = append(out, Credit{Name: name, RemainingCents: number(remaining), ExpiresAt: at})
	}
	return out, nil
}
