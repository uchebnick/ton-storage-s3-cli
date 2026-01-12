package ton

import (
	"context"
	"strings"
	"math/rand"
	"time"
)

var KnownProviders = []string{
	"94059ff6f0e534b571797a41d2203c500b305e924333ff2264ae20ca0976f9f7",
	"0f40a7b9c0d22294214bfee73a31051189b7728ac006b9264c8e963d18279c34",
	"7a156635aeae3a97e2947ba88385390e7aefbb70d789d2f6916c9423d4697f3a",
}


func (s *Service) FindRandomProvider(ctx context.Context, exclude []string) (string, error) {

	excludedMap := make(map[string]bool)
	for _, addr := range exclude {
		if len(addr) > 64 && strings.Contains(addr, ":") {
			parts := strings.Split(addr, ":")
			if len(parts) == 2 {
				excludedMap[parts[1]] = true
			}
		} else {
			excludedMap[addr] = true
		}
	}

	var candidates []string
	for _, provider := range KnownProviders {
		if !excludedMap[provider] {
			candidates = append(candidates, provider)
		}
	}

	if len(candidates) == 0 {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		return KnownProviders[rng.Intn(len(KnownProviders))], nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return candidates[rng.Intn(len(candidates))], nil
}