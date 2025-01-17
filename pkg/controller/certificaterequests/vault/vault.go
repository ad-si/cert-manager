/*
Copyright 2019 The Jetstack cert-manager contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vault

import (
	"context"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	corelisters "k8s.io/client-go/listers/core/v1"

	apiutil "github.com/jetstack/cert-manager/pkg/api/util"
	"github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	controllerpkg "github.com/jetstack/cert-manager/pkg/controller"
	"github.com/jetstack/cert-manager/pkg/controller/certificaterequests"
	crutil "github.com/jetstack/cert-manager/pkg/controller/certificaterequests/util"
	vaultinternal "github.com/jetstack/cert-manager/pkg/internal/vault"
	"github.com/jetstack/cert-manager/pkg/issuer"
	logf "github.com/jetstack/cert-manager/pkg/logs"
	"github.com/jetstack/cert-manager/pkg/util/api"
)

const (
	CRControllerName = "certificaterequests-issuer-vault"
)

type Vault struct {
	secretsLister corelisters.SecretLister
	reporter      *crutil.Reporter

	vaultClientBuilder vaultinternal.VaultClientBuilder
}

func init() {
	// create certificate request controller for vault issuer
	controllerpkg.Register(CRControllerName, func(ctx *controllerpkg.Context) (controllerpkg.Interface, error) {
		vault := NewVault(ctx)

		controller := certificaterequests.New(apiutil.IssuerVault, vault)

		c, err := controllerpkg.New(ctx, CRControllerName, controller)
		if err != nil {
			return nil, err
		}

		return c.Run, nil
	})
}

func NewVault(ctx *controllerpkg.Context) *Vault {
	return &Vault{
		secretsLister:      ctx.KubeSharedInformerFactory.Core().V1().Secrets().Lister(),
		reporter:           crutil.NewReporter(ctx.Clock, ctx.Recorder),
		vaultClientBuilder: vaultinternal.New,
	}
}

func (v *Vault) Sign(ctx context.Context, cr *v1alpha1.CertificateRequest, issuerObj v1alpha1.GenericIssuer) (*issuer.IssueResponse, error) {
	log := logf.FromContext(ctx, "sign")
	log = logf.WithRelatedResource(log, issuerObj)

	client, err := v.vaultClientBuilder(cr.Namespace, v.secretsLister, issuerObj)
	if k8sErrors.IsNotFound(err) {
		message := "Required secret resource not found"

		v.reporter.Pending(cr, err, "MissingSecret", message)
		log.Error(err, message)
		return nil, nil
	}

	if err != nil {
		message := "Failed to initialise vault client for signing"
		v.reporter.Pending(cr, err, "ErrorVaultInit", message)
		log.Error(err, message)
		return nil, err
	}

	certDuration := api.DefaultCertDuration(cr.Spec.Duration)
	certPem, caPem, err := client.Sign(cr.Spec.CSRPEM, certDuration)
	if err != nil {
		message := "Vault failed to sign certificate"

		v.reporter.Failed(cr, err, "ErrorSigning", message)
		log.Error(err, message)

		return nil, nil
	}

	log.Info("certificate issued")

	return &issuer.IssueResponse{
		Certificate: certPem,
		CA:          caPem,
	}, nil
}
