// Package bundle handles extraction and parsing of Kubernetes support bundles.
package bundle

// k8sPodList is a minimal JSON representation of a Kubernetes PodList.
type k8sPodList struct {
	Items []k8sPod `json:"items"`
}

type k8sPod struct {
	Metadata k8sObjectMeta `json:"metadata"`
	Spec     k8sPodSpec    `json:"spec"`
	Status   k8sPodStatus  `json:"status"`
}

type k8sObjectMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type k8sPodSpec struct {
	NodeName string `json:"nodeName"`
}

type k8sPodStatus struct {
	Phase                 string               `json:"phase"`
	ContainerStatuses     []k8sContainerStatus `json:"containerStatuses"`
	InitContainerStatuses []k8sContainerStatus `json:"initContainerStatuses"`
}

type k8sContainerStatus struct {
	Name         string            `json:"name"`
	Ready        bool              `json:"ready"`
	RestartCount int               `json:"restartCount"`
	State        k8sContainerState `json:"state"`
	LastState    k8sContainerState `json:"lastState"`
}

type k8sContainerState struct {
	Waiting    *k8sStateWaiting    `json:"waiting"`
	Terminated *k8sStateTerminated `json:"terminated"`
}

type k8sStateWaiting struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type k8sStateTerminated struct {
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	ExitCode int    `json:"exitCode"`
}

// k8sNodeList is a minimal JSON representation of a Kubernetes NodeList.
type k8sNodeList struct {
	Items []k8sNode `json:"items"`
}

type k8sNode struct {
	Metadata k8sObjectMeta `json:"metadata"`
	Status   k8sNodeStatus `json:"status"`
}

type k8sNodeStatus struct {
	Conditions []k8sNodeCondition `json:"conditions"`
	Capacity   map[string]string  `json:"capacity"`
}

type k8sNodeCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// k8sEventList is a minimal JSON representation of a Kubernetes EventList.
type k8sEventList struct {
	Items []k8sEvent `json:"items"`
}

type k8sEvent struct {
	Metadata       k8sObjectMeta `json:"metadata"`
	InvolvedObject k8sObjectRef  `json:"involvedObject"`
	Reason         string        `json:"reason"`
	Message        string        `json:"message"`
	Type           string        `json:"type"`
	Count          int32         `json:"count"`
	FirstTimestamp string        `json:"firstTimestamp"`
	LastTimestamp  string        `json:"lastTimestamp"`
	EventTime      string        `json:"eventTime"`
}

type k8sObjectRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}
