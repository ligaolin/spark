// Package sshconn resolves saved-connection credential references (managed
// keys) and jump chains (ProxyJump) into ready-to-dial ConnectOptions.
package sshconn

import (
	"strings"

	"spark/app/model"
	"spark/app/service/db"
	"spark/app/service/types"
)

const maxJumpDepth = 8

// Resolve fills a managed-key reference into PrivateKey/Passphrase and expands
// opts.JumpID into opts.Jump (outermost hop first), following each jump host's
// own JumpID. Cycles and missing connections stop the chain (the remaining hops
// are kept as already resolved).
func Resolve(opts types.ConnectOptions) types.ConnectOptions {
	opts = resolveKey(opts)
	if opts.JumpID == 0 || len(opts.Jump) > 0 {
		return opts
	}
	opts.Jump = chainFor(opts.JumpID)
	return opts
}

// resolveKey loads the managed key referenced by KeyID when the caller did not
// inline a private key. Missing keys are tolerated: dialing then fails with the
// usual "私钥内容不能为空" error.
func resolveKey(opts types.ConnectOptions) types.ConnectOptions {
	if opts.KeyID == 0 || !opts.UseKey || strings.TrimSpace(opts.PrivateKey) != "" {
		return opts
	}
	var key model.SSHKey
	if err := db.GetDB().First(&key, opts.KeyID).Error; err != nil {
		return opts
	}
	opts.PrivateKey = key.PrivateKey
	opts.Passphrase = key.Passphrase
	return opts
}

// OptionsFromSaved maps a saved connection to dial options (its managed key and
// jump chain are resolved later by Resolve).
func OptionsFromSaved(cn model.SavedConnection) types.ConnectOptions {
	return types.ConnectOptions{
		Host:         cn.Host,
		Port:         cn.Port,
		Username:     cn.Username,
		Password:     cn.Password,
		UseKey:       cn.UseKey,
		PrivateKey:   cn.PrivateKey,
		Passphrase:   cn.Passphrase,
		ForwardAgent: cn.ForwardAgent,
		JumpID:       cn.JumpID,
		KeyID:        cn.KeyID,
	}
}

// chainFor collects jump hops starting at id, returning them outermost first.
// jump_id points to the immediate jump host, whose own jump_id is the next
// hop outward, so the collected order is reversed before returning.
func chainFor(id uint) []types.ConnectOptions {
	var hops []types.ConnectOptions
	seen := make(map[uint]bool)
	cur := id
	for cur != 0 && len(hops) < maxJumpDepth {
		if seen[cur] {
			break
		}
		seen[cur] = true
		var cn model.SavedConnection
		if err := db.GetDB().First(&cn, cur).Error; err != nil {
			break
		}
		hops = append(hops, resolveKey(OptionsFromSaved(cn)))
		cur = cn.JumpID
	}
	for i, j := 0, len(hops)-1; i < j; i, j = i+1, j-1 {
		hops[i], hops[j] = hops[j], hops[i]
	}
	return hops
}