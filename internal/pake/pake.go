// Package pake implements SPAKE2 over NIST P-256, allowing two parties that
// share a low-entropy password to derive a strong shared key without ever
// putting the password (or anything brute-forceable offline) on the wire.
package pake

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
)

// Role distinguishes the two sides of the exchange; they must differ.
type Role int

const (
	RoleA Role = iota // sender
	RoleB             // receiver
)

var curve = elliptic.P256()

// m and n are the two fixed, nothing-up-my-sleeve points required by SPAKE2.
// They are derived by hashing a fixed label and mapping onto the curve, so
// nobody knows their discrete logs.
var mX, mY = hashToPoint("croc-go SPAKE2 point M")
var nX, nY = hashToPoint("croc-go SPAKE2 point N")

// hashToPoint maps a label onto the curve by try-and-increment.
func hashToPoint(label string) (*big.Int, *big.Int) {
	p := curve.Params().P
	for i := 0; i < 1000; i++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", label, i)))
		x := new(big.Int).SetBytes(sum[:])
		x.Mod(x, p)
		if y, ok := decompress(x); ok {
			return x, y
		}
	}
	panic("pake: could not derive curve point")
}

// decompress returns a y such that y^2 = x^3 - 3x + b, if one exists.
func decompress(x *big.Int) (*big.Int, bool) {
	p := curve.Params().P
	y2 := new(big.Int).Mul(x, x)
	y2.Mod(y2, p)
	y2.Mul(y2, x)
	y2.Sub(y2, new(big.Int).Mul(big.NewInt(3), x))
	y2.Add(y2, curve.Params().B)
	y2.Mod(y2, p)

	y := new(big.Int).ModSqrt(y2, p)
	if y == nil {
		return nil, false
	}
	return y, true
}

// Pake holds one party's state across the two-message exchange.
type Pake struct {
	role     Role
	password []byte
	scalar   *big.Int // our ephemeral secret x
	msgX     *big.Int // our public share, sent to the peer
	msgY     *big.Int
	key      []byte
}

// New starts an exchange for the given role and shared password.
func New(role Role, password []byte) (*Pake, error) {
	x, err := rand.Int(rand.Reader, curve.Params().N)
	if err != nil {
		return nil, err
	}

	// T = x*G + w*(M or N), where w is the password mapped to a scalar.
	gx, gy := curve.ScalarBaseMult(x.Bytes())
	bx, by := blindPoint(role)
	w := passwordScalar(password)
	wx, wy := curve.ScalarMult(bx, by, w.Bytes())
	tx, ty := curve.Add(gx, gy, wx, wy)

	return &Pake{role: role, password: password, scalar: x, msgX: tx, msgY: ty}, nil
}

// blindPoint returns the mask point this role adds to its share.
func blindPoint(role Role) (*big.Int, *big.Int) {
	if role == RoleA {
		return mX, mY
	}
	return nX, nY
}

func passwordScalar(password []byte) *big.Int {
	sum := sha256.Sum256(append([]byte("croc-go pw|"), password...))
	w := new(big.Int).SetBytes(sum[:])
	return w.Mod(w, curve.Params().N)
}

// Bytes returns the message to hand to the peer.
func (p *Pake) Bytes() []byte {
	return elliptic.MarshalCompressed(curve, p.msgX, p.msgY)
}

// Update consumes the peer's message and derives the shared key. It fails if
// the peer's share is not a valid curve point, but note that a wrong password
// yields a different key rather than an error: the mismatch surfaces when the
// key confirmation step fails.
func (p *Pake) Update(peer []byte) error {
	px, py := elliptic.UnmarshalCompressed(curve, peer)
	if px == nil {
		return errors.New("pake: peer sent an invalid curve point")
	}

	// Strip the peer's mask, then multiply by our secret to reach the
	// same point both sides compute: x*y*G.
	bx, by := blindPoint(otherRole(p.role))
	w := passwordScalar(p.password)
	negW := new(big.Int).Sub(curve.Params().N, w)
	ux, uy := curve.ScalarMult(bx, by, negW.Bytes())
	sx, sy := curve.Add(px, py, ux, uy)
	kx, ky := curve.ScalarMult(sx, sy, p.scalar.Bytes())

	// Hash a transcript that both sides order identically, so A and B agree.
	first, second := p.Bytes(), peer
	if p.role == RoleB {
		first, second = peer, p.Bytes()
	}
	h := sha256.New()
	h.Write([]byte("croc-go session key"))
	h.Write(first)
	h.Write(second)
	h.Write(elliptic.MarshalCompressed(curve, kx, ky))
	h.Write(w.Bytes())
	p.key = h.Sum(nil)
	return nil
}

func otherRole(r Role) Role {
	if r == RoleA {
		return RoleB
	}
	return RoleA
}

// SessionKey returns the derived 32-byte key, valid only after Update.
func (p *Pake) SessionKey() ([]byte, error) {
	if p.key == nil {
		return nil, errors.New("pake: exchange is not complete")
	}
	return p.key, nil
}
