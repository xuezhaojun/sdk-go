package csr

import (
	"context"

	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
)

type csr interface {
	*certificatesv1.CertificateSigningRequest | *certificatesv1beta1.CertificateSigningRequest
}

type csrLister[T csr] interface {
	Get(name string) (T, error)
}

type csrApprover[T csr] interface {
	approve(ctx context.Context, csr T) approveCSRFunc
	isInTerminalState(csr T) bool
}

type approveCSRFunc func(kubernetes.Interface) error
