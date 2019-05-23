package businesspolicy

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/open-horizon/anax/externalpolicy"
	_ "github.com/open-horizon/anax/externalpolicy/text_language"
	"github.com/open-horizon/anax/policy"
)

const DEFAULT_MAX_AGREEMENT = 5

// the business policy
type BusinessPolicy struct {
	Owner       string                              `json:"owner,omitempty"`
	Label       string                              `json:"label"`
	Description string                              `json:"description"`
	Service     ServiceRef                          `json:"service"`
	Properties  externalpolicy.PropertyList         `json:"properties,omitempty"`
	Constraints externalpolicy.ConstraintExpression `json:"constraints,omitempty"`
}

func (w BusinessPolicy) String() string {
	return fmt.Sprintf("Owner: %v, Label: %v, Description: %v, Service: %v, Properties: %v, Constraints: %v",
		w.Owner,
		w.Label,
		w.Description,
		w.Service,
		w.Properties,
		w.Constraints)
}

type ServiceRef struct {
	Name            string           `json:"name"`                      // refers to a service definition in the exchange
	Org             string           `json:"org,omitempty"`             // the org holding the service definition
	Arch            string           `json:"arch,omitempty"`            // the hardware architecture of the service definition
	ServiceVersions []WorkloadChoice `json:"serviceVersions,omitempty"` // a list of service version for rollback
	NodeH           NodeHealth       `json:"nodeHealth"`                // policy for determining when a node's health is violating its agreements
}

func (w ServiceRef) String() string {
	return fmt.Sprintf("Name: %v, Org: %v, Arch: %v, ServiceVersions: %v, NodeH: %v",
		w.Name,
		w.Org,
		w.Arch,
		w.ServiceVersions,
		w.NodeH)
}

type WorkloadPriority struct {
	PriorityValue     int `json:"priority_value,omitempty"`     // The priority of the workload
	Retries           int `json:"retries,omitempty"`            // The number of retries before giving up and moving to the next priority
	RetryDurationS    int `json:"retry_durations,omitempty"`    // The number of seconds in which the specified number of retries must occur in order for the next priority workload to be attempted.
	VerifiedDurationS int `json:"verified_durations,omitempty"` // The number of second in which verified data must exist before the rollback retry feature is turned off
}

func (w WorkloadPriority) String() string {
	return fmt.Sprintf("PriorityValue: %v, Retries: %v, RetryDurationS: %v, VerifiedDurationS: %v",
		w.PriorityValue,
		w.Retries,
		w.RetryDurationS,
		w.VerifiedDurationS)
}

type UpgradePolicy struct {
	Lifecycle string `json:"lifecycle,omitempty"` // immediate, never, agreement
	Time      string `json:"time,omitempty"`      // the time of the upgrade
}

func (w UpgradePolicy) String() string {
	return fmt.Sprintf("Lifecycle: %v, Time: %v",
		w.Lifecycle,
		w.Time)
}

type WorkloadChoice struct {
	Version  string           `json:"version,omitempty"`  // the version of the workload
	Priority WorkloadPriority `json:"priority,omitempty"` // the highest priority workload is tried first for an agreement, if it fails, the next priority is tried. Priority 1 is the highest, priority 2 is next, etc.
	Upgrade  UpgradePolicy    `json:"upgradePolicy,omitempty"`
}

func (w WorkloadChoice) String() string {
	return fmt.Sprintf("Version: %v, Priority: %v, Upgrade: %v",
		w.Version,
		w.Priority,
		w.Upgrade)
}

type NodeHealth struct {
	MissingHBInterval    int `json:"missing_heartbeat_interval,omitempty"` // How long a heartbeat can be missing until it is considered missing (in seconds)
	CheckAgreementStatus int `json:"check_agreement_status,omitempty"`     // How often to check that the node agreement entry still exists in the exchange (in seconds)
}

func (w NodeHealth) String() string {
	return fmt.Sprintf("MissingHBInterval: %v, CheckAgreementStatus: %v",
		w.MissingHBInterval,
		w.CheckAgreementStatus)
}

// The validate function returns errors if the policy does not validate. It uses the constraint language
// plugins to handle the constraints field.
func (b *BusinessPolicy) Validate() error {

	// make sure required fields are not empty
	if b.Service.Name == "" || b.Service.Org == "" || b.Service.Arch == "" {
		return fmt.Errorf("Name, Org or Arch is empty string.")
	} else if b.Service.ServiceVersions == nil || len(b.Service.ServiceVersions) == 0 {
		return fmt.Errorf("The serviceVersions array is empty.")
	}

	// Validate the PropertyList.
	if b != nil && len(b.Properties) != 0 {
		if err := b.Properties.Validate(); err != nil {
			return fmt.Errorf(fmt.Sprintf("properties contains an invalid property: %v", err))
		}
	}

	// Validate the Constraints expression by invoking the plugins.
	if b != nil && len(b.Constraints) != 0 {
		return b.Constraints.Validate()
	}

	// We only get here if the input object is nil OR all of the top level fields are empty.
	return nil
}

// Convert a pattern to a list of policy objects. Each pattern contains 1 or more workloads or services,
// which will each be translated to a policy.
func (b *BusinessPolicy) GenPolicyFromBusinessPolicy(policyName string) (*policy.Policy, error) {

	// validate first
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("Failed to validate the business policy: %v", err)
	}

	service := b.Service
	pol := policy.Policy_Factory(fmt.Sprintf("%v", policyName))

	// Copy service metadata into the policy
	for _, wl := range service.ServiceVersions {
		if wl.Version == "" {
			return nil, fmt.Errorf("The version for service %v arch %v is empty in the business policy for %v", service.Name, service.Arch, policyName)
		}
		ConvertChoice(wl, service.Name, service.Org, service.Arch, pol)
	}

	// properties and constrains
	if err := ConvertProperties(b.Properties, pol); err != nil {
		return nil, err
	}
	if err := ConvertConstraints(b.Constraints, pol); err != nil {
		return nil, err
	}

	// node health
	ConvertNodeHealth(service.NodeH, pol)

	pol.MaxAgreements = DEFAULT_MAX_AGREEMENT

	glog.V(3).Infof("converted %v into %v", service, pol)

	return pol, nil
}

func ConvertChoice(wl WorkloadChoice, url string, org string, arch string, pol *policy.Policy) {
	newWL := policy.Workload_Factory(url, org, wl.Version, arch)
	newWL.Priority = (*policy.Workload_Priority_Factory(wl.Priority.PriorityValue, wl.Priority.Retries, wl.Priority.RetryDurationS, wl.Priority.VerifiedDurationS))
	pol.Add_Workload(newWL)
}

func ConvertNodeHealth(nodeh NodeHealth, pol *policy.Policy) {
	// Copy over the node health policy
	nh := policy.NodeHealth_Factory(nodeh.MissingHBInterval, nodeh.CheckAgreementStatus)
	pol.Add_NodeHealth(nh)
}

func ConvertProperties(properties externalpolicy.PropertyList, pol *policy.Policy) error {
	for _, p := range properties {
		if err := pol.Add_Property(&p); err != nil {
			return fmt.Errorf("error trying add external policy property %v to policy. %v", p, err)
		}
	}
	return nil
}

func ConvertConstraints(constraints externalpolicy.ConstraintExpression, pol *policy.Policy) error {
	// Copy over the node health policy
	rp, err := policy.RequiredPropertyFromConstraint(&constraints)
	if err != nil {
		return fmt.Errorf("error trying to convert external policy constraints to JSON: %v", err)
	}
	if rp != nil {
		pol.CounterPartyProperties = (*rp)
	}
	return nil
}