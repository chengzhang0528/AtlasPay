package alipay

import (
	"testing"

	"github.com/atlaspay/meridian-gateway/internal/domain"
)

func TestResolveEnvironmentUsesOnlyOfficialGateways(t *testing.T) {
	tests := []struct {
		value       domain.AlipayEnvironment
		wantEnv     domain.AlipayEnvironment
		wantGateway string
		wantErr     bool
	}{
		{value: " sandbox ", wantEnv: domain.AlipayEnvironmentSandbox, wantGateway: SandboxGatewayURL},
		{value: "PRODUCTION", wantEnv: domain.AlipayEnvironmentProduction, wantGateway: ProductionGatewayURL},
		{value: "custom", wantErr: true},
		{value: "", wantErr: true},
	}
	for _, test := range tests {
		t.Run(string(test.value), func(t *testing.T) {
			gotEnv, gotGateway, err := ResolveEnvironment(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ResolveEnvironment(%q) succeeded", test.value)
				}
				return
			}
			if err != nil || gotEnv != test.wantEnv || gotGateway != test.wantGateway {
				t.Fatalf("ResolveEnvironment(%q) = %q %q %v", test.value, gotEnv, gotGateway, err)
			}
			mapped, err := EnvironmentForGatewayURL(gotGateway)
			if err != nil || mapped != test.wantEnv {
				t.Fatalf("EnvironmentForGatewayURL(%q) = %q %v", gotGateway, mapped, err)
			}
		})
	}
}
