// Copyright 2020 ConsenSys Software Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plonk

import (
	"math/big"

	"github.com/consensys/gnark/crypto/polynomial"
	"github.com/consensys/gnark/crypto/polynomial/bn256"
	"github.com/consensys/gnark/internal/backend/bn256/cs"
	"github.com/consensys/gnark/internal/backend/bn256/fft"
	bn256witness "github.com/consensys/gnark/internal/backend/bn256/witness"
	"github.com/consensys/gurvy/bn256/fr"
)

// TODO derive those random values using Fiat Shamir
// zeta: value at which l, r, o, h are evaluated
// vBundle: challenge used to bundle opening proofs at a single point (l+vBundle.r + vBundle**2*o + ...)
// gamma: used in (l+X+gamma)*(r+u.X+gamma).(o.u**2X+gamma)
// alpha: used in qlL+qrR+qmL.R+qoO+k + alpha.(Z(uX)g1g2g3-Z(X)f1f2f3) + alpha**2L1(Z-1) = HZ
var zeta, vBundle, gamma, alpha fr.Element

func init() {
	zeta.SetString("2938092839238274283")
	vBundle.SetString("987545678")
	gamma.SetString("8278263826")
	alpha.SetString("25678323434")
}

// Proof PLONK proofs, consisting of opening proofs
type Proof struct {

	// Claimed Values are the values of L,R,O,H,Z at zeta
	LROHZ [5]fr.Element

	// Claimed vales of Z(zX) at zeta
	ZShift fr.Element

	// batch opening proofs for L,R,O,H,Z at zeta
	BatchOpenings polynomial.BatchOpeningProofSinglePoint

	// opening proof for Z at z*zeta
	OpeningZShift polynomial.OpeningProof
}

// ComputeLRO extracts the solution l, r, o, and returns it in lagrange form.
func ComputeLRO(spr *cs.SparseR1CS, publicData *PublicRaw, solution []fr.Element) (bn256.Poly, bn256.Poly, bn256.Poly) {

	s := int(publicData.DomainNum.Cardinality)

	var l, r, o bn256.Poly
	l = make([]fr.Element, s)
	r = make([]fr.Element, s)
	o = make([]fr.Element, s)

	for i := 0; i < len(spr.Constraints); i++ {
		l[i].Set(&solution[spr.Constraints[i].L.VariableID()])
		r[i].Set(&solution[spr.Constraints[i].R.VariableID()])
		o[i].Set(&solution[spr.Constraints[i].O.VariableID()])
	}
	offset := len(spr.Constraints)
	for i := 0; i < len(spr.Assertions); i++ {
		l[offset+i].Set(&solution[spr.Assertions[i].L.VariableID()])
		r[offset+i].Set(&solution[spr.Assertions[i].R.VariableID()])
		o[offset+i].Set(&solution[spr.Assertions[i].O.VariableID()])
	}

	// the padded constraints are dummy constraints -> the variable ID is 0 in those
	// constraints. We therefore need to add solution[0] to l, r, o once we reach the
	// dummy constraint, so that l, r, o is compliant with the permutation.
	offset += len(spr.Assertions)
	for i := 0; i < s-offset; i++ {
		l[offset+i].Set(&solution[0])
		r[offset+i].Set(&solution[0])
		o[offset+i].Set(&solution[0])
	}

	return l, r, o

}

// ComputeZ computes Z (in Lagrange basis), where:
//
// * Z of degree n (domainNum.Cardinality)
// * Z(1)=1
// 								  (l_i+z**i+gamma)*(r_i+u*z**i+gamma)*(o_i+u**2z**i+gamma)
//	* for i>1: Z(u**i) = Pi_{k<i} -------------------------------------------------------
//								  (l_i+s1+gamma)*(r_i+s2+gamma)*(o_i+s3+gamma)
//
//	* l, r, o are the solution in Lagrange basis
func ComputeZ(l, r, o bn256.Poly, publicData *PublicRaw) bn256.Poly {

	z := make(bn256.Poly, publicData.DomainNum.Cardinality)
	nbElmts := int(publicData.DomainNum.Cardinality)

	var f [3]fr.Element
	var g [3]fr.Element
	var u [3]fr.Element
	u[0].SetOne()
	u[1].Set(&publicData.Shifter[0])
	u[2].Square(&publicData.Shifter[1])

	z[0].SetOne()

	for i := 0; i < nbElmts-1; i++ {

		f[0].Add(&l[i], &u[0]).Add(&f[0], &gamma) //l_i+z**i+gamma
		f[1].Add(&r[i], &u[1]).Add(&f[1], &gamma) //r_i+u*z**i+gamma
		f[2].Add(&o[i], &u[2]).Add(&f[2], &gamma) //o_i+u**2*z**i+gamma

		u[0].Mul(&u[0], &publicData.DomainNum.Generator) // z**i -> z**i+1
		u[1].Mul(&u[1], &publicData.DomainNum.Generator) // u*z**i -> u*z**i+1
		u[2].Mul(&u[2], &publicData.DomainNum.Generator) // u**2*z**i -> u**2*z**i+1

		g[0].Add(&l[i], &publicData.LS1[i]).Add(&g[0], &gamma) //l_i+z**i+gamma
		g[1].Add(&r[i], &publicData.LS2[i]).Add(&g[1], &gamma) //r_i+u*z**i+gamma
		g[2].Add(&o[i], &publicData.LS3[i]).Add(&g[2], &gamma) //o_i+u**2*z**i+gamma

		f[0].Mul(&f[0], &f[1]).Mul(&f[0], &f[2]) // (l_i+z**i+gamma)*(r_i+u*z**i+gamma)*(o_i+u**2z**i+gamma)
		g[0].Mul(&g[0], &g[1]).Mul(&g[0], &g[2]) //  (l_i+s1+gamma)*(r_i+s2+gamma)*(o_i+s3+gamma)

		z[i+1].Mul(&z[i], &f[0]).Div(&z[i+1], &g[0])
	}

	return z

}

// evalConstraints computes the evaluation of lL+qrR+qqmL.R+qoO+k on
// the odd cosets 1 of (Z/8mZ)/(Z/mZ), where m=nbConstraints+nbAssertions.
//
// qlL+qrR+qmL.R+qoO+k = H*Z, where Z=x^n-1
//
// l, r, o are the evaluation of l,r,o on the odd cosets of (Z/8mZ)/(Z/mZ)
func evalConstraints(publicData *PublicRaw, evalL, evalR, evalO []fr.Element) []fr.Element {

	// evaluates ql, qr, qm, qo, k on the odd cosets of (Z/8mZ)/(Z/mZ)
	evalQl := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalQr := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalQm := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalQo := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalQk := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evaluateCosets(publicData.Ql, evalQl, publicData.DomainNum)
	evaluateCosets(publicData.Qr, evalQr, publicData.DomainNum)
	evaluateCosets(publicData.Qm, evalQm, publicData.DomainNum)
	evaluateCosets(publicData.Qo, evalQo, publicData.DomainNum)
	evaluateCosets(publicData.Qk, evalQk, publicData.DomainNum)

	// computes the evaluation of qrR+qlL+qmL.R+qoO+k on the odd cosets
	// of (Z/8mZ)/(Z/mZ)
	var acc, buf fr.Element
	for i := uint64(0); i < 4*publicData.DomainNum.Cardinality; i++ {

		acc.Mul(&evalQl[i], &evalL[i]) // ql.l

		buf.Mul(&evalQr[i], &evalR[i])
		acc.Add(&acc, &buf) // ql.l + qr.r

		buf.Mul(&evalQm[i], &evalL[i]).Mul(&buf, &evalR[i])
		acc.Add(&acc, &buf) // ql.l + qr.r + qm.l.r

		buf.Mul(&evalQo[i], &evalO[i])
		acc.Add(&acc, &buf)            // ql.l + qr.r + qm.l.r + qo.o
		evalL[i].Add(&acc, &evalQk[i]) // ql.l + qr.r + qm.l.r + qo.o + k
	}

	return evalL
}

// evalIDCosets id, uid, u**2id on the odd cosets of (Z/8mZ)/(Z/mZ)
func evalIDCosets(publicData *PublicRaw) (id, uid, uuid bn256.Poly) {

	// evaluation of id, uid, u**id on the cosets
	id = make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	c := int(publicData.DomainNum.Cardinality)
	id[0].SetOne()
	id[1].SetOne()
	id[2].SetOne()
	id[3].SetOne()
	for i := 1; i < c; i++ {
		id[4*i].Mul(&id[4*(i-1)], &publicData.DomainNum.Generator)
		id[4*i+1].Set(&id[4*i])
		id[4*i+2].Set(&id[4*i])
		id[4*i+3].Set(&id[4*i])
	}
	// at this stage, id = [1,1,1,1,|z,z,z,z|,...,|z**n-1,z**n-1,z**n-1,z**n-1]

	var uu fr.Element
	uu.Square(&publicData.DomainNum.FinerGenerator)
	var u [4]fr.Element
	u[0].Set(&publicData.DomainNum.FinerGenerator)                // u
	u[1].Mul(&u[0], &uu)                                          // u**3
	u[2].Mul(&u[1], &uu)                                          // u**5
	u[3].Mul(&u[2], &uu)                                          // u**7
	uid = make([]fr.Element, 4*publicData.DomainNum.Cardinality)  // shifter[0]*ID evaluated on odd cosets of (Z/8mZ)/(Z/mZ)
	uuid = make([]fr.Element, 4*publicData.DomainNum.Cardinality) // shifter[1]*ID evaluated on odd cosets of (Z/8mZ)/(Z/mZ)
	for i := 0; i < c; i++ {

		id[4*i].Mul(&id[4*i], &u[0])     // coset u.<1,z,..,z**n-1>
		id[4*i+1].Mul(&id[4*i+1], &u[1]) // coset u**3.<1,z,..,z**n-1>
		id[4*i+2].Mul(&id[4*i+2], &u[2]) // coset u**5.<1,z,..,z**n-1>
		id[4*i+3].Mul(&id[4*i+3], &u[3]) // coset u**7.<1,z,..,z**n-1>

		uid[4*i].Mul(&id[4*i], &publicData.Shifter[0])     // shifter[0]*ID
		uid[4*i+1].Mul(&id[4*i+1], &publicData.Shifter[0]) // shifter[0]*ID
		uid[4*i+2].Mul(&id[4*i+2], &publicData.Shifter[0]) // shifter[0]*ID
		uid[4*i+3].Mul(&id[4*i+3], &publicData.Shifter[0]) // shifter[0]*ID

		uuid[i].Mul(&id[i], &publicData.Shifter[1])         // shifter[1]*ID
		uuid[i+c].Mul(&id[i+c], &publicData.Shifter[1])     // shifter[1]*ID
		uuid[i+2*c].Mul(&id[i+2*c], &publicData.Shifter[1]) // shifter[1]*ID
		uuid[i+3*c].Mul(&id[i+3*c], &publicData.Shifter[1]) // shifter[1]*ID

	}

	return

}

// evalZ computes the evaluation of Z(uX)g1g2g3-Z(X)f1f2f3 on the odd
// cosets of (Z/8mZ)/(Z/mZ), where m=nbConstraints+nbAssertions.
//
// z: permutation accumulator polynomial in canonical form
// l, r, o: solution, in canonical form
func evalConstraintOrdering(publicData *PublicRaw, z, zu, evalL, evalR, evalO bn256.Poly) bn256.Poly {

	// evaluation of z, zu, s1, s2, s3, on the odd cosets of (Z/8mZ)/(Z/mZ)
	evalZ := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalZu := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalS1 := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalS2 := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalS3 := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evaluateCosets(z, evalZ, publicData.DomainNum)
	evaluateCosets(zu, evalZu, publicData.DomainNum)
	evaluateCosets(publicData.CS1, evalS1, publicData.DomainNum)
	evaluateCosets(publicData.CS2, evalS2, publicData.DomainNum)
	evaluateCosets(publicData.CS3, evalS3, publicData.DomainNum)

	// evalutation of ID, u*ID, u**2*ID on the odd cosets of (Z/8mZ)/(Z/mZ)
	evalID, evaluID, evaluuID := evalIDCosets(publicData)

	// computes Z(uX)g1g2g3l-Z(X)f1f2f3l on the odd cosets of (Z/8mZ)/(Z/mZ)
	res := make(bn256.Poly, 4*publicData.DomainNum.Cardinality)

	var f [3]fr.Element
	var g [3]fr.Element
	for i := 0; i < 4*int(publicData.DomainNum.Cardinality); i++ {

		f[0].Add(&evalL[i], &evalID[i]).Add(&f[0], &gamma)   //l_i+z**i+gamma
		f[1].Add(&evalR[i], &evaluID[i]).Add(&f[1], &gamma)  //r_i+u*z**i+gamma
		f[2].Add(&evalO[i], &evaluuID[i]).Add(&f[2], &gamma) //o_i+u**2*z**i+gamma

		g[0].Add(&evalL[i], &evalS1[i]).Add(&g[0], &gamma) //l_i+s1+gamma
		g[1].Add(&evalR[i], &evalS2[i]).Add(&g[1], &gamma) //r_i+s2+gamma
		g[2].Add(&evalO[i], &evalS3[i]).Add(&g[2], &gamma) //o_i+s3+gamma

		f[0].Mul(&f[0], &f[1]).
			Mul(&f[0], &f[2]).
			Mul(&f[0], &evalL[i]).
			Mul(&f[0], &evalZ[i]) // z_i*(l_i+z**i+gamma)*(r_i+u*z**i+gamma)*(o_i+u**2*z**i+gamma)*l_i

		g[0].Mul(&g[0], &g[1]).
			Mul(&g[0], &g[2]).
			Mul(&g[0], &evalL[i]).
			Mul(&g[0], &evalZu[i]) // u*z_i*(l_i+z**i+gamma)*(r_i+u*z**i+gamma)*(o_i+u**2*z**i+gamma)*l_i

		res[i].Sub(&g[0], &f[0])
	}

	return res
}

// evaluateCosets evaluates poly (canonical form) of degree m=domainNum.Cardinality on
// the 4 odd cosets of (Z/8mZ)/(Z/mZ), so it dodges Z/mZ (+Z/2kmZ), which contains the
// vanishing set of Z.
//
// Puts the result in res (of size 4*domain.Cardinality).
//
// Both sizes of poly and res are powers of 2, len(res) = 4*len(poly).
func evaluateCosets(poly, res []fr.Element, domain *fft.Domain) {

	// build a copy of poly padded with 0 so it has the length of the closest power of 2 of poly
	evaluations := make([][]fr.Element, 4)
	evaluations[0] = make([]fr.Element, domain.Cardinality)
	evaluations[1] = make([]fr.Element, domain.Cardinality)
	evaluations[2] = make([]fr.Element, domain.Cardinality)
	evaluations[3] = make([]fr.Element, domain.Cardinality)

	// evaluations[i] must contain poly in the canonical basis
	copy(evaluations[0], poly)
	copy(evaluations[1], poly)
	copy(evaluations[2], poly)
	copy(evaluations[3], poly)

	domain.FFT(evaluations[0], fft.DIF, 1)
	domain.FFT(evaluations[1], fft.DIF, 3)
	domain.FFT(evaluations[0], fft.DIF, 5)
	domain.FFT(evaluations[1], fft.DIF, 7)
	fft.BitReverse(evaluations[0])
	fft.BitReverse(evaluations[1])
	fft.BitReverse(evaluations[2])
	fft.BitReverse(evaluations[3])

	for i := uint64(0); i < domain.Cardinality; i++ {
		res[4*i].Set(&evaluations[0][i])
		res[4*i+1].Set(&evaluations[1][i])
		res[4*i+2].Set(&evaluations[2][i])
		res[4*i+3].Set(&evaluations[3][i])
	}
}

// shiftZ turns z to z(uX) (both in Lagrange basis)
func shiftZ(z bn256.Poly) bn256.Poly {

	res := make(bn256.Poly, len(z))
	copy(res, z)

	var buf fr.Element
	buf.Set(&res[0])
	for i := 0; i < len(res)-1; i++ {
		res[i].Set(&res[i+1])
	}
	res[len(res)-1].Set(&buf)

	return res
}

// computeH computes h (canonical form) such that
//
// qlL+qrR+qmL.R+qoO+k + alpha.(zu*g1*g2*g3*l-z*f1*f2*f3*l) = h.Z
// \------------------/         \------------------------/
//    constraintsInd			    constraintOrdering
//
// constraintInd, constraintOrdering are evaluated on the odd cosets of (Z/8mZ)/(Z/mZ)
func computeH(publicData *PublicRaw, constraintsInd, constraintOrdering bn256.Poly) bn256.Poly {

	h := make(bn256.Poly, 4*publicData.DomainNum.Cardinality)

	// evaluate qlL+qrR+qmL.R+qoO+k + alpha.(zu*g1*g2*g3*l-z*f1*f2*f3*l)
	// on the odd cosets of (Z/8mZ)/(Z/mZ)
	var buf fr.Element
	for i := 0; i < 4*int(publicData.DomainNum.Cardinality); i++ {
		buf.Mul(&alpha, &constraintOrdering[i])
		h[i].Add(&constraintsInd[i], &buf)
	}

	// evaluate Z = X**m-1 on the odd cosets of (Z/8mZ)/(Z/mZ)
	var bExpo big.Int
	bExpo.SetUint64(publicData.DomainNum.Cardinality)
	var u [4]fr.Element
	var uu fr.Element
	uu.Square(&publicData.DomainNum.FinerGenerator)
	u[0].Set(&publicData.DomainNum.FinerGenerator).
		Exp(u[0], &bExpo).
		Inverse(&u[0]) // (u**m)**-1
	u[1].Mul(&u[0], &uu).
		Exp(u[1], &bExpo).
		Inverse(&u[1]) // (u**3)**-m
	u[2].Mul(&u[1], &uu).
		Exp(u[2], &bExpo).
		Inverse(&u[2]) // (u**5)**-m
	u[3].Mul(&u[2], &uu).
		Exp(u[3], &bExpo).
		Inverse(&u[3]) // (u**7)**-m

	// evaluate qlL+qrR+qmL.R+qoO+k + alpha.(zu*g1*g2*g3*l-z*f1*f2*f3*l)/Z
	// on the odd cosets of (Z/8mZ)/(Z/mZ)
	for i := 0; i < int(publicData.DomainNum.Cardinality); i++ {
		h[4*i].Mul(&h[4*i], &u[0])
		h[4*i+1].Mul(&h[4*i+1], &u[1])
		h[4*i+2].Mul(&h[4*i+2], &u[2])
		h[4*i+3].Mul(&h[4*i+3], &u[3])
	}

	// put h in canonical form
	publicData.DomainH.FFTInverse(h, fft.DIF, 1)
	fft.BitReverse(h)

	return h

}

// Prove from the public data
// TODO add a parameter to force the resolution of the system even if a constraint does not hold
func Prove(spr *cs.SparseR1CS, publicData *PublicRaw, witness bn256witness.Witness) *Proof {

	// compute the solution
	solution, _ := spr.Solve(witness)

	// query l, r, o in Lagrange basis
	l, r, o := ComputeLRO(spr, publicData, solution)

	// compute the evaluations of l, r, o on odd cosets of (Z/8mZ)/(Z/mZ)
	evalL := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalR := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evalO := make([]fr.Element, 4*publicData.DomainNum.Cardinality)
	evaluateCosets(publicData.Ql, evalL, publicData.DomainNum)
	evaluateCosets(publicData.Qr, evalR, publicData.DomainNum)
	evaluateCosets(publicData.Qm, evalO, publicData.DomainNum)

	// compute the evaluation of qlL+qrR+qmL.R+qoO+k on the odd cosets of (Z/8mZ)/(Z/mZ)
	constraintsInd := evalConstraints(publicData, evalL, evalR, evalO)

	// compute Z, the permutation accumulator polynomial, in Lagrange basis
	z := ComputeZ(l, r, o, publicData)

	// compute Z(uX), in Lagrange basis
	zu := shiftZ(z)

	// put back z, zu in canonical basis
	publicData.DomainNum.FFTInverse(z, fft.DIF, 0)
	publicData.DomainNum.FFTInverse(zu, fft.DIF, 0)
	fft.BitReverse(z)
	fft.BitReverse(zu)

	// compute zu*g1*g2*g3*l-z*f1*f2*f3*l on the odd cosets of (Z/8mZ)/(Z/mZ)
	//
	// /!\ IMPORTANT NOTE /!\
	//
	// l is added so that zu*g1*g2*g3*l-z*f1*f2*f3*l is a degree 5m polynomial,
	// so when dividing it by x^m-1, we obtain a degree 4m polynomial h, so we can
	// perform radix 2 fft to evaluate h on 4m points. l is not divisible by h, so
	// it does not impact the security of the scheme.
	constraintsOrdering := evalConstraintOrdering(publicData, z, zu, evalL, evalR, evalO)

	// compute h (its evaluation)
	h := computeH(publicData, constraintsInd, constraintsOrdering)

	// put l, r, o,  in canonical basis
	publicData.DomainNum.FFTInverse(l, fft.DIF, 0)
	publicData.DomainNum.FFTInverse(r, fft.DIF, 0)
	publicData.DomainNum.FFTInverse(o, fft.DIF, 0)
	fft.BitReverse(l)
	fft.BitReverse(r)
	fft.BitReverse(o)

	// compute evaluations of l, r, o, h, z at zeta
	proof := &Proof{}
	tmp := l.Eval(&zeta)
	proof.LROHZ[0].Set(tmp.(*fr.Element))
	tmp = r.Eval(&zeta)
	proof.LROHZ[1].Set(tmp.(*fr.Element))
	tmp = o.Eval(&zeta)
	proof.LROHZ[2].Set(tmp.(*fr.Element))
	tmp = h.Eval(&zeta)
	proof.LROHZ[3].Set(tmp.(*fr.Element))
	tmp = z.Eval(&zeta)
	proof.LROHZ[4].Set(tmp.(*fr.Element))

	// compute evaluation of z at z*zeta
	var zzeta fr.Element
	zzeta.Mul(&zeta, &publicData.DomainNum.Generator)
	tmp = z.Eval(&zzeta)
	proof.ZShift.Set(tmp.(*fr.Element))

	// compute batch opening proof for l, r, o, h, z at zeta
	proof.BatchOpenings = publicData.CommitmentScheme.BatchOpenSinglePoint(&zeta, &vBundle, l, r, o, h, z)

	// compute opening proof for z at z*zeta
	proof.OpeningZShift = publicData.CommitmentScheme.Open(&zzeta, z)

	return &Proof{}
}
