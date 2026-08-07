package alipay

import (
	"fmt"
	"strings"

	"github.com/atlaspay/meridian-gateway/internal/domain"
)

const (
	SandboxGatewayURL    = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
	ProductionGatewayURL = "https://openapi.alipay.com/gateway.do"
)

func ResolveEnvironment(value domain.AlipayEnvironment) (domain.AlipayEnvironment, string, error) {
	environment := domain.AlipayEnvironment(strings.ToLower(strings.TrimSpace(string(value))))
	switch environment {
	case domain.AlipayEnvironmentSandbox:
		return environment, SandboxGatewayURL, nil
	case domain.AlipayEnvironmentProduction:
		return environment, ProductionGatewayURL, nil
	default:
		return "", "", fmt.Errorf("alipay environment must be sandbox or production")
	}
}

func EnvironmentForGatewayURL(value string) (domain.AlipayEnvironment, error) {
	switch strings.TrimSpace(value) {
	case SandboxGatewayURL:
		return domain.AlipayEnvironmentSandbox, nil
	case ProductionGatewayURL:
		return domain.AlipayEnvironmentProduction, nil
	default:
		return "", fmt.Errorf("unsupported alipay gateway URL")
	}
}
