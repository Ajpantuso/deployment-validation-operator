package controller

import (
	"strings"

	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

func reconcileResourceList(client discovery.DiscoveryInterface, scheme *runtime.Scheme) ([]metav1.APIResource, error) {
	apiResources := []metav1.APIResource{}
	apiResourceSet := map[schema.GroupKind]metav1.APIResource{}

	_, apiResourceLists, err := client.ServerGroupsAndResources()
	if err != nil {
		return nil, err
	}

	for _, apiResourceList := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			return nil, err
		}
		for _, apiResource := range apiResourceList.APIResources {
			// skip sub resources (pods/scale, deployment/status...)
			if isSubResource(&apiResource) {
				continue
			}
			apiResource.Group = gv.Group
			apiResource.Version = gv.Version

			canValidate, err := isRegisteredKubeLinterKind(apiResource)
			if err != nil {
				return nil, err
			}
			if !canValidate {
				continue
			}

			if !isRegisteredInScheme(&apiResource, scheme) {
				continue
			}

			gk := schema.GroupKind{
				Group: apiResource.Group,
				Kind:  apiResource.Kind,
			}
			existing, ok := apiResourceSet[gk]
			if !ok {
				apiResourceSet[gk] = apiResource
				continue
			}
			priorityGV := getPriorityVersion(existing.Group, existing.Version, apiResource.Version, scheme)
			existing.Version = priorityGV
			apiResourceSet[gk] = existing
		}
	}
	for _, v := range apiResourceSet {
		apiResources = append(apiResources, v)
	}
	return apiResources, nil
}

func getPriorityVersion(group, existingVer, currentVer string, scheme *runtime.Scheme) string {
	gv := scheme.PrioritizedVersionsAllGroups()
	for _, v := range gv {
		if v.Version == existingVer {
			return existingVer
		}
		if v.Version == currentVer {
			return currentVer
		}
	}
	return existingVer
}

// isSubResource returns true if the apiResource.Name has a "/" in it eg: pod/status
func isSubResource(apiResource *metav1.APIResource) bool {
	return strings.Contains(apiResource.Name, "/")
}

func isRegisteredKubeLinterKind(rsrc metav1.APIResource) (bool, error) {
	// Construct the gvks for objects to watch.  Remove the Any
	// kind or else all objects kinds will be watched.
	kubeLinterKinds := getKubeLinterKinds()
	kubeLinterMatcher, err := objectkinds.ConstructMatcher(kubeLinterKinds...)
	if err != nil {
		return false, err
	}

	gvk := gvkFromMetav1APIResource(rsrc)
	if kubeLinterMatcher.Matches(gvk) {
		return true, nil
	}
	return false, nil
}

func getKubeLinterKinds() []string {
	kubeLinterKinds := objectkinds.AllObjectKinds()
	for i := range kubeLinterKinds {
		if kubeLinterKinds[i] == objectkinds.Any {
			kubeLinterKinds = append(kubeLinterKinds[:i], kubeLinterKinds[i+1:]...)
			break
		}
	}
	return kubeLinterKinds
}

func gvkFromMetav1APIResource(rsc metav1.APIResource) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   rsc.Group,
		Version: rsc.Version,
		Kind:    rsc.Kind,
	}
}

func isRegisteredInScheme(apiResource *metav1.APIResource, scheme *runtime.Scheme) bool {
	gvk := schema.GroupVersionKind{
		Group:   apiResource.Group,
		Version: apiResource.Version,
		Kind:    apiResource.Kind,
	}
	return scheme.Recognizes(gvk)
}
