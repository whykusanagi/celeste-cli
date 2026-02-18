// Package skills provides crypto skills registration
package skills

// RegisterCryptoSkills registers all cryptocurrency and blockchain skills
func RegisterCryptoSkills(registry *Registry, configLoader ConfigLoader) {
	// Register IPFS skill
	registry.RegisterSkill(IPFSSkill())
	registry.RegisterHandler("ipfs", func(args map[string]interface{}) (interface{}, error) {
		return IPFSHandler(args, configLoader)
	})

	// Register Alchemy skill
	registry.RegisterSkill(AlchemySkill())
	registry.RegisterHandler("alchemy", func(args map[string]interface{}) (interface{}, error) {
		return AlchemyHandler(args, configLoader)
	})

	// Register Blockchain Monitoring skill
	registry.RegisterSkill(BlockmonSkill())
	registry.RegisterHandler("blockmon", func(args map[string]interface{}) (interface{}, error) {
		return BlockmonHandler(args, configLoader)
	})

	// Register Wallet Security skill
	registry.RegisterSkill(WalletSecuritySkill())
	registry.RegisterHandler("wallet_security", func(args map[string]interface{}) (interface{}, error) {
		return WalletSecurityHandler(args, configLoader)
	})
}
