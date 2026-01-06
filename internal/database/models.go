package database

import "time"

type File struct {
	ID		int64
	BucketName	string
	ObjectKey	string
	BagID		string
	SizeBytes	int64
	TargetReplicas	int
	Status		string
	CreatedAt	time.Time
}

type Contract struct {
	ID		int64
	FileID		int64
	ProviderAddr	string
	ContractAddr	string
	BalanceNano	int64
	Status		string
	LastCheck	time.Time
}

type ContractWithMeta struct {
	Contract
	BagID	string
}

type FileWithStatus struct {
	File
	ActiveReplicas	int
	UsedProviders	[]string
}
