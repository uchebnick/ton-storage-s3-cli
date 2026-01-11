package ton

import (
	"context"
	"strings"
	"math/rand"
	"time"
)

var KnownProviders = []string{
	"bc0b88cb8df9c7c595f0ec50960489cb5471f935415e2ee3e389466503b2281d",
	"41a9cd79f55b0abe00b68df838384fbebf16ea11d0f24a8a480f907a93bf33c9",
	"085cd07d69470d56df81f441d5748e34d657c804efefc496c1065123ad5fec02",
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