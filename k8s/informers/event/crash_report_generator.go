package event

import (
	"code.cloudfoundry.org/eirini/events"
	"code.cloudfoundry.org/eirini/k8s"
	"code.cloudfoundry.org/eirini/util"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/runtimeschema/cc_messages"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type DefaultCrashReportGenerator struct{}

func (DefaultCrashReportGenerator) Generate(pod *v1.Pod, clientset kubernetes.Interface, logger lager.Logger) (events.CrashReport, bool) {
	statuses := pod.Status.ContainerStatuses
	if len(statuses) == 0 {
		return events.CrashReport{}, false
	}

	_, err := util.ParseAppIndex(pod.Name)
	if err != nil {
		logger.Error("failed-to-parse-app-index", err, lager.Data{"pod-name": pod.Name, "guid": pod.Annotations[k8s.AnnotationProcessGUID]})
		return events.CrashReport{}, false
	}

	if status := getTerminatedContainerStatusIfAny(pod.Status.ContainerStatuses); status != nil {
		return generateReportForTerminatedPod(pod, status, clientset, logger)
	}

	if container := getCrashedContainerStatusIfAny(pod.Status.ContainerStatuses); container != nil {
		exitStatus := int(container.LastTerminationState.Terminated.ExitCode)
		exitDescription := container.LastTerminationState.Terminated.Reason
		crashTimestamp := int64(container.LastTerminationState.Terminated.StartedAt.Second())
		return generateReport(pod, container.State.Waiting.Reason, exitStatus, exitDescription, crashTimestamp, int(container.RestartCount))
	}
	return events.CrashReport{}, false
}

func generateReportForTerminatedPod(pod *v1.Pod, status *v1.ContainerStatus, clientset kubernetes.Interface, logger lager.Logger) (events.CrashReport, bool) {
	podEvents, err := k8s.GetEvents(clientset.CoreV1().Events(pod.Namespace), *pod)
	if err != nil {
		logger.Error("failed-to-get-k8s-events", err, lager.Data{"guid": pod.Annotations[k8s.AnnotationProcessGUID]})
		return events.CrashReport{}, false
	}
	if k8s.IsStopped(podEvents) {
		return events.CrashReport{}, false
	}

	terminated := status.State.Terminated

	return generateReport(pod, terminated.Reason, int(terminated.ExitCode), terminated.Reason, int64(terminated.StartedAt.Second()), int(status.RestartCount))
}

func generateReport(
	pod *v1.Pod,
	reason string,
	exitStatus int,
	exitDescription string,
	crashTimestamp int64,
	restartCount int,
) (events.CrashReport, bool) {
	index, _ := util.ParseAppIndex(pod.Name)

	return events.CrashReport{
		ProcessGUID: pod.Annotations[k8s.AnnotationProcessGUID],
		AppCrashedRequest: cc_messages.AppCrashedRequest{
			Reason:          reason,
			Instance:        pod.Name,
			Index:           index,
			ExitStatus:      exitStatus,
			ExitDescription: exitDescription,
			CrashTimestamp:  crashTimestamp,
			CrashCount:      restartCount,
		},
	}, true
}

func getTerminatedContainerStatusIfAny(statuses []v1.ContainerStatus) *v1.ContainerStatus {
	for _, status := range statuses {
		terminated := status.State.Terminated
		if terminated != nil && terminated.ExitCode != 0 {
			return &status
		}
	}

	return nil
}

func getCrashedContainerStatusIfAny(statuses []v1.ContainerStatus) *v1.ContainerStatus {
	for _, status := range statuses {
		waiting := status.State.Waiting
		if waiting != nil && waiting.Reason == CrashLoopBackOff {
			return &status
		}
	}

	return nil
}
