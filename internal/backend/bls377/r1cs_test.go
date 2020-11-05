// Copyright 2020 ConsenSys AG
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

// Code generated by gnark/internal/generators DO NOT EDIT

package backend_test

import (
	bls377backend "github.com/consensys/gnark/internal/backend/bls377"

	"bytes"
	"reflect"
	"testing"

	"github.com/consensys/gnark/internal/backend/circuits"
	"github.com/consensys/gurvy"
)

func TestSerialization(t *testing.T) {
	for name, circuit := range circuits.Circuits {
		t.Run(name, func(t *testing.T) {
			r1cs := circuit.R1CS.ToR1CS(gurvy.BLS377)
			var buffer bytes.Buffer
			var err error
			var written, read int64
			written, err = r1cs.WriteTo(&buffer)
			if err != nil {
				t.Fatal(err)
			}
			var reconstructed bls377backend.R1CS
			read, err = reconstructed.ReadFrom(&buffer)
			if err != nil {
				t.Fatal(err)
			}
			if written != read {
				t.Fatal("didn't read same number of bytes we wrote")
			}
			// compare both
			if !reflect.DeepEqual(r1cs, &reconstructed) {
				t.Fatal("round trip serialization failed")
			}
		})
	}
}
