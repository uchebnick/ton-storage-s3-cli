package ton

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

var KnownProviders = []string{
	"EQCtCEcQeulPvo89ohIySxB0XT0RdON_7c9qkziic59G9mg6",
	"EQDJlWIX-7HcN-7RqDhFhpuCfbHLruOMJ2p4hKKr_AN7_R9S",
	"EQBQZHQElR4NsG1LpxcgDs28uan8mC4oIRdxJcnmHdYKxKWC",
	"EQD6bNCmOioAWowK6ee5f0_wt_9ZVEK2_4h0dbSfUqxtJdC3",
	"EQDSDRwBN8vv0TvGRd9dh7Cq9B_EToqCWNmQHJS7UlDA82y7",
	"EQDqtIbnphcjx310zHrn7o9FwD-ToWTb1psIH5IDoyEadQsB",
	"EQDrqJ_AbapwD-38KOI8qJmbLexmmxKbh2ngYs95Lq2A8Uw7",
	"EQDKX25ZfT6rik45LBXoxXMycfBJDw-TV256wF6ZmNAEjoz0",
}

func (s *Service) FindRandomProvider(ctx context.Context, exclude []string) (string, error) {

	excludedMap := make(map[string]bool)
	for _, addr := range exclude {
		excludedMap[addr] = true
	}

	var candidates []string
	for _, provider := range KnownProviders {

		if !excludedMap[provider] {
			candidates = append(candidates, provider)
		}
	}

	if len(candidates) == 0 {
		return "", errors.New("no available providers left (all known providers are already used)")
	}

	rand.Seed(time.Now().UnixNano())
	randomIndex := rand.Intn(len(candidates))

	return candidates[randomIndex], nil
}
