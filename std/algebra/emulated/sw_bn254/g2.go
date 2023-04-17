package sw_bn254

import (
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/fields_bn254"
	"github.com/consensys/gnark/std/math/emulated"
)

type G2 struct {
	*fields_bn254.Ext2
}

type G2Affine struct {
	X, Y fields_bn254.E2
}

func NewG2(api frontend.API) G2 {
	return G2{
		Ext2: fields_bn254.NewExt2(api),
	}
}

func NewG2Affine(v bn254.G2Affine) G2Affine {
	return G2Affine{
		X: fields_bn254.E2{
			A0: emulated.ValueOf[emulated.BN254Fp](v.X.A0),
			A1: emulated.ValueOf[emulated.BN254Fp](v.X.A1),
		},
		Y: fields_bn254.E2{
			A0: emulated.ValueOf[emulated.BN254Fp](v.Y.A0),
			A1: emulated.ValueOf[emulated.BN254Fp](v.Y.A1),
		},
	}
}

func (g2 *G2) psi(q *G2Affine) *G2Affine {
	u := fields_bn254.E2{
		A0: emulated.ValueOf[emulated.BN254Fp]("21575463638280843010398324269430826099269044274347216827212613867836435027261"),
		A1: emulated.ValueOf[emulated.BN254Fp]("10307601595873709700152284273816112264069230130616436755625194854815875713954"),
	}
	v := fields_bn254.E2{
		A0: emulated.ValueOf[emulated.BN254Fp]("2821565182194536844548159561693502659359617185244120367078079554186484126554"),
		A1: emulated.ValueOf[emulated.BN254Fp]("3505843767911556378687030309984248845540243509899259641013678093033130930403"),
	}

	var psiq G2Affine
	psiq.X = *g2.Ext2.Conjugate(&q.X)
	psiq.X = *g2.Ext2.Mul(&psiq.X, &u)
	psiq.Y = *g2.Ext2.Conjugate(&q.Y)
	psiq.Y = *g2.Ext2.Mul(&psiq.Y, &v)

	return &psiq
}

func (g2 *G2) scalarMulBySeed(q *G2Affine) *G2Affine {
	// TODO: use 2-NAF or addchain (or maybe a ternary expansion)
	var seed = [63]int8{1, 0, 0, 0, 1, 1, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 1, 1, 0, 0, 1, 0, 1, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 1}

	// i = 62
	res := q

	for i := 61; i >= 0; i-- {
		res = g2.double(res)
		if seed[i] == 1 {
			// TODO: use doubleAndAdd
			res = g2.add(res, q)
		}
	}

	return res
}

func (g2 G2) add(p, q *G2Affine) *G2Affine {
	// compute λ = (q.y-p.y)/(q.x-p.x)
	qypy := g2.Ext2.Sub(&q.Y, &p.Y)
	qxpx := g2.Ext2.Sub(&q.X, &p.X)
	// TODO: use Div
	qxpx = g2.Ext2.Inverse(qxpx)
	λ := g2.Ext2.Mul(qypy, qxpx)

	// xr = λ²-p.x-q.x
	λλ := g2.Ext2.Mul(λ, λ)
	qxpx = g2.Ext2.Add(&p.X, &q.X)
	xr := g2.Ext2.Sub(λλ, qxpx)

	// p.y = λ(p.x-r.x) - p.y
	pxrx := g2.Ext2.Sub(&p.X, xr)
	λpxrx := g2.Ext2.Mul(λ, pxrx)
	yr := g2.Ext2.Sub(λpxrx, &p.Y)

	return &G2Affine{
		X: *xr,
		Y: *yr,
	}
}

func (g2 G2) neg(p *G2Affine) *G2Affine {
	xr := &p.X
	yr := g2.Ext2.Neg(&p.Y)
	return &G2Affine{
		X: *xr,
		Y: *yr,
	}
}

func (g2 G2) sub(p, q *G2Affine) *G2Affine {
	qNeg := g2.neg(q)
	return g2.add(p, qNeg)
}

func (g2 *G2) double(p *G2Affine) *G2Affine {
	// compute λ = (3p.x²)/2*p.y
	xx3a := g2.Mul(&p.X, &p.X)
	xx3a = g2.MulByConstElement(xx3a, big.NewInt(3))
	y2 := g2.Double(&p.Y)
	// TODO: use Div
	y2 = g2.Inverse(y2)
	λ := g2.Mul(xx3a, y2)

	// xr = λ²-2p.x
	x2 := g2.Double(&p.X)
	λλ := g2.Mul(λ, λ)
	xr := g2.Sub(λλ, x2)

	// yr = λ(p-xr) - p.y
	pxrx := g2.Sub(&p.X, xr)
	λpxrx := g2.Mul(λ, pxrx)
	yr := g2.Sub(λpxrx, &p.Y)

	return &G2Affine{
		X: *xr,
		Y: *yr,
	}
}

// AssertIsEqual asserts that p and q are the same point.
func (g2 *G2) AssertIsEqual(p, q *G2Affine) {
	g2.Ext2.AssertIsEqual(&p.X, &q.X)
	g2.Ext2.AssertIsEqual(&p.Y, &q.Y)
}
