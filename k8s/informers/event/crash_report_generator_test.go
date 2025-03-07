package event_test

import (
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/eirini/events"
	"code.cloudfoundry.org/eirini/k8s"
	"code.cloudfoundry.org/eirini/k8s/informers/event"
	"code.cloudfoundry.org/lager/lagertest"
	"code.cloudfoundry.org/runtimeschema/cc_messages"
	v1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	testcore "k8s.io/client-go/testing"
)

var crashTime = meta.Time{Time: time.Now()}

var _ = Describe("CrashReportGenerator", func() {
	var (
		client *fake.Clientset
		logger *lagertest.TestLogger
		pod    *v1.Pod
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("crash-event-logger-test")
		client = fake.NewSimpleClientset()
	})

	Context("When app is in CrashLoopBackOff", func() {
		Context("When there is one container in the pod", func() {
			BeforeEach(func() {
				pod = newCrashedPod()
			})

			It("should return a crashed report", func() {
				generator := event.DefaultCrashReportGenerator{}
				report, returned := generator.Generate(pod, client, logger)
				Expect(returned).To(BeTrue())
				Expect(report).To(Equal(events.CrashReport{
					ProcessGUID: "test-pod-anno",
					AppCrashedRequest: cc_messages.AppCrashedRequest{
						Reason:          event.CrashLoopBackOff,
						Instance:        "test-pod-0",
						Index:           0,
						ExitStatus:      1,
						ExitDescription: "better luck next time",
						CrashCount:      3,
						CrashTimestamp:  int64(crashTime.Time.Second()),
					},
				}))
			})
		})

		Context("When there are multiple containers in the pod", func() {
			BeforeEach(func() {
				pod = newMultiContainerCrashedPod()
			})

			It("should return a crashed report", func() {
				generator := event.DefaultCrashReportGenerator{}
				report, returned := generator.Generate(pod, client, logger)
				Expect(returned).To(BeTrue())
				Expect(report).To(Equal(events.CrashReport{
					ProcessGUID: "test-pod-anno",
					AppCrashedRequest: cc_messages.AppCrashedRequest{
						Reason:          event.CrashLoopBackOff,
						Instance:        "test-pod-0",
						Index:           0,
						ExitStatus:      1,
						ExitDescription: "better luck next time",
						CrashCount:      3,
						CrashTimestamp:  int64(crashTime.Time.Second()),
					},
				}))
			})
		})
	})

	Context("When app has been terminated", func() {
		Context("When there is one container in the pod", func() {
			BeforeEach(func() {
				pod = newTerminatedPod()
			})

			It("should generate a crashed report", func() {
				generator := event.DefaultCrashReportGenerator{}
				report, returned := generator.Generate(pod, client, logger)
				Expect(returned).To(BeTrue())
				Expect(report).To(Equal(events.CrashReport{
					ProcessGUID: "test-pod-anno",
					AppCrashedRequest: cc_messages.AppCrashedRequest{
						Reason:          "better luck next time",
						Instance:        "test-pod-0",
						Index:           0,
						ExitStatus:      1,
						ExitDescription: "better luck next time",
						CrashCount:      8,
						CrashTimestamp:  int64(crashTime.Time.Second()),
					},
				}))
			})

			Context("with zero exit status", func() {

				BeforeEach(func() {
					pod.Status.ContainerStatuses[0].State.Terminated.ExitCode = 0
				})

				It("should not generate the report", func() {
					generator := event.DefaultCrashReportGenerator{}
					_, returned := generator.Generate(pod, client, logger)
					Expect(returned).To(BeFalse())
				})

			})

			Context("When a pod name does not have index", func() {

				BeforeEach(func() {
					pod.Name = "naughty-pod"
				})

				It("should not generate", func() {
					generator := event.DefaultCrashReportGenerator{}
					_, returned := generator.Generate(pod, client, logger)
					Expect(returned).To(BeFalse())
				})

				It("should provide a helpful log message", func() {
					generator := event.DefaultCrashReportGenerator{}
					generator.Generate(pod, client, logger)

					logs := logger.Logs()
					Expect(logs).To(HaveLen(1))
					log := logs[0]
					Expect(log.Message).To(Equal("crash-event-logger-test.failed-to-parse-app-index"))
					Expect(log.Data).To(HaveKeyWithValue("pod-name", "naughty-pod"))
					Expect(log.Data).To(HaveKeyWithValue("guid", "test-pod-anno"))
				})

			})

			Context("When pod is stopped", func() {

				BeforeEach(func() {
					event := v1.Event{
						InvolvedObject: v1.ObjectReference{
							Namespace: "not-default",
							Name:      "pinky-pod",
						},
						Reason: "Killing",
					}
					_, clientErr := client.CoreV1().Events("not-default").Create(&event)
					Expect(clientErr).ToNot(HaveOccurred())
				})

				It("should not emit a crashed event", func() {
					generator := event.DefaultCrashReportGenerator{}
					_, returned := generator.Generate(pod, client, logger)
					Expect(returned).To(BeFalse())
				})
			})

			Context("When getting events fails", func() {
				BeforeEach(func() {
					reaction := func(action testcore.Action) (handled bool, ret runtime.Object, err error) {
						return true, nil, errors.New("boom")
					}
					client.PrependReactor("list", "events", reaction)
				})

				It("should not emit a crashed event", func() {
					generator := event.DefaultCrashReportGenerator{}
					_, returned := generator.Generate(pod, client, logger)
					Expect(returned).To(BeFalse())
				})

				It("should provide a helpful log message", func() {
					generator := event.DefaultCrashReportGenerator{}
					generator.Generate(pod, client, logger)
					logs := logger.Logs()
					Expect(logs).To(HaveLen(1))
					log := logs[0]
					Expect(log.Message).To(Equal("crash-event-logger-test.failed-to-get-k8s-events"))
					Expect(log.Data).To(HaveKeyWithValue("guid", "test-pod-anno"))
				})
			})
		})
		Context("When there are multiple containers in the pod", func() {
			BeforeEach(func() {
				pod = newMultiContainerTerminatedPod()
			})

			It("should generate a crashed report", func() {
				generator := event.DefaultCrashReportGenerator{}
				report, returned := generator.Generate(pod, client, logger)
				Expect(returned).To(BeTrue())
				Expect(report).To(Equal(events.CrashReport{
					ProcessGUID: "test-pod-anno",
					AppCrashedRequest: cc_messages.AppCrashedRequest{
						Reason:          "better luck next time",
						Instance:        "test-pod-0",
						Index:           0,
						ExitStatus:      1,
						ExitDescription: "better luck next time",
						CrashCount:      8,
						CrashTimestamp:  int64(crashTime.Time.Second()),
					},
				}))
			})
		})
	})

	Context("When app is waiting, but NOT because of CrashLoopBackOff", func() {

		BeforeEach(func() {
			pod = newCrashedPod()
			pod.Status.ContainerStatuses[0].State.Waiting.Reason = "Friday"
		})

		It("should not send reports", func() {
			generator := event.DefaultCrashReportGenerator{}
			_, returned := generator.Generate(pod, client, logger)
			Expect(returned).To(BeFalse())
		})

	})

	Context("When a pod has no container statuses", func() {

		BeforeEach(func() {
			pod = newCrashedPod()
		})

		Context("container statuses is nil", func() {
			BeforeEach(func() {
				pod.Status.ContainerStatuses = nil
			})

			It("should not send any reports", func() {
				generator := event.DefaultCrashReportGenerator{}
				_, returned := generator.Generate(pod, client, logger)
				Expect(returned).To(BeFalse())
			})
		})

		Context("container statuses is empty", func() {
			BeforeEach(func() {
				pod.Status.ContainerStatuses = []v1.ContainerStatus{}
			})

			It("should not send any reports", func() {
				generator := event.DefaultCrashReportGenerator{}
				_, returned := generator.Generate(pod, client, logger)
				Expect(returned).To(BeFalse())
			})
		})
	})

})

func newTerminatedPod() *v1.Pod {
	return newPod([]v1.ContainerStatus{
		{
			RestartCount: 8,
			State: v1.ContainerState{
				Terminated: &v1.ContainerStateTerminated{
					Reason:    "better luck next time",
					StartedAt: crashTime,
					ExitCode:  1,
				},
			},
		},
	})
}

func newMultiContainerTerminatedPod() *v1.Pod {
	return newPod([]v1.ContainerStatus{
		{
			RestartCount: 1,
			State: v1.ContainerState{
				Running: &v1.ContainerStateRunning{},
			},
		},
		{
			RestartCount: 8,
			State: v1.ContainerState{
				Terminated: &v1.ContainerStateTerminated{
					Reason:    "better luck next time",
					StartedAt: crashTime,
					ExitCode:  1,
				},
			},
		},
	})
}

func newCrashedPod() *v1.Pod {
	return newPod([]v1.ContainerStatus{
		{
			RestartCount: 3,
			State: v1.ContainerState{
				Waiting: &v1.ContainerStateWaiting{
					Reason: event.CrashLoopBackOff,
				},
			},
			LastTerminationState: v1.ContainerState{
				Terminated: &v1.ContainerStateTerminated{
					ExitCode:  1,
					Reason:    "better luck next time",
					StartedAt: crashTime,
				},
			},
		},
	})
}

func newMultiContainerCrashedPod() *v1.Pod {
	return newPod([]v1.ContainerStatus{
		{
			RestartCount: 1,
			State: v1.ContainerState{
				Running: &v1.ContainerStateRunning{},
			},
		},
		{
			RestartCount: 3,
			State: v1.ContainerState{
				Waiting: &v1.ContainerStateWaiting{
					Reason: event.CrashLoopBackOff,
				},
			},
			LastTerminationState: v1.ContainerState{
				Terminated: &v1.ContainerStateTerminated{
					ExitCode:  1,
					Reason:    "better luck next time",
					StartedAt: crashTime,
				},
			},
		},
	})
}

func newPod(statuses []v1.ContainerStatus) *v1.Pod {
	name := "test-pod"
	return &v1.Pod{
		ObjectMeta: meta.ObjectMeta{
			Name: fmt.Sprintf("%s-%d", name, 0),
			Annotations: map[string]string{
				k8s.AnnotationProcessGUID: fmt.Sprintf("%s-anno", name),
			},
			OwnerReferences: []meta.OwnerReference{
				{
					Kind: "StatefulSet",
					Name: "mr-stateful",
				},
			},
		},
		Status: v1.PodStatus{
			ContainerStatuses: statuses,
		},
	}
}
