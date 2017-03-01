package util

import (
	"fmt"
	"io/ioutil"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/pkg/api"
	_ "k8s.io/client-go/pkg/api/install"
	"k8s.io/client-go/pkg/api/v1"
	_ "k8s.io/client-go/pkg/apis/apps/install"
	appsv1beta1 "k8s.io/client-go/pkg/apis/apps/v1beta1"
	_ "k8s.io/client-go/pkg/apis/extensions/install"
	extensionsv1beta1 "k8s.io/client-go/pkg/apis/extensions/v1beta1"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/hcl"
)

func makeCodec(contentType string, pretty bool) (runtime.Codec, error) {
	serializerInfo, ok := runtime.SerializerInfoForMediaType(
		api.Codecs.SupportedMediaTypes(),
		contentType,
	)

	if !ok {
		return nil, fmt.Errorf("unable to create a serializer")
	}

	serializer := serializerInfo.Serializer

	if pretty && serializerInfo.PrettySerializer != nil {
		serializer = serializerInfo.PrettySerializer
	}

	codec := api.Codecs.CodecForVersions(
		serializer,
		serializer,
		schema.GroupVersions(
			[]schema.GroupVersion{
				v1.SchemeGroupVersion,
				extensionsv1beta1.SchemeGroupVersion,
				appsv1beta1.SchemeGroupVersion,
			},
		),
		runtime.InternalGroupVersioner,
	)

	return codec, nil
}

func deleteKeyIfValueIsNil(obj map[string]interface{}, key string) {
	if v, ok := obj[key]; ok {
		if v == nil {
			delete(obj, key)
		}
	}
}

func deleteSubKeyIfValueIsNil(obj map[string]interface{}, k0, k1 string) {
	if v, ok := obj[k0]; ok {
		if v := v.(map[string]interface{}); len(v) != 0 {
			deleteKeyIfValueIsNil(v, k1)
		}
	}
	deleteKeyIfValueIsEmptyMap(obj, k0)
}

func deleteKeyIfValueIsEmptyMap(obj map[string]interface{}, key string) {
	if v, ok := obj[key]; ok {
		if v := v.(map[string]interface{}); len(v) == 0 {
			delete(obj, key)
		}
	}
}

func deleteSubKeyIfValueIsEmptyMap(obj map[string]interface{}, k0, k1 string) {
	if v, ok := obj[k0]; ok {
		if v := v.(map[string]interface{}); len(v) != 0 {
			deleteKeyIfValueIsEmptyMap(v, k1)
		}
	}
	deleteKeyIfValueIsEmptyMap(obj, k0)
}

func cleanup(contentType string, input []byte) ([]byte, error) {
	obj := make(map[string]interface{})
	switch contentType {
	case "application/yaml":
		if err := yaml.Unmarshal(input, &obj); err != nil {
			return nil, err
		}

		deleteKeyIfValueIsEmptyMap(obj, "metadata")
		if items, ok := obj["items"]; ok {
			if items := items.([]interface{}); len(items) != 0 {
				for _, item := range items {
					if item := item.(map[string]interface{}); len(item) != 0 {
						deleteSubKeyIfValueIsNil(item, "metadata", "creationTimestamp")
						deleteSubKeyIfValueIsEmptyMap(item, "status", "loadBalancer")

						deleteSubKeyIfValueIsEmptyMap(item, "spec", "strategy")

						if spec, ok := item["spec"]; ok {
							if spec := spec.(map[string]interface{}); len(spec) != 0 {
								if template, ok := spec["template"]; ok {
									if template := template.(map[string]interface{}); len(template) != 0 {
										if spec, ok := template["spec"]; ok {
											if spec := spec.(map[string]interface{}); len(spec) != 0 {
												if containers, ok := spec["containers"]; ok {
													if containers := containers.([]interface{}); len(containers) != 0 {
														for _, container := range containers {
															if container := container.(map[string]interface{}); len(container) != 0 {
																deleteKeyIfValueIsEmptyMap(container, "resources")
																deleteKeyIfValueIsEmptyMap(container, "securityContext")
															}
														}
													}
												}
											}
										}
										deleteSubKeyIfValueIsNil(template, "metadata", "creationTimestamp")
									}
								}
							}
						}
					}
				}
			}
		}

		output, err := yaml.Marshal(obj)
		if err != nil {
			return nil, err
		}
		return output, nil
	default:
		return input, nil
	}

}

func Encode(object runtime.Object, contentType string, pretty bool) ([]byte, error) {
	codec, err := makeCodec(contentType, pretty)
	if err != nil {
		return nil, fmt.Errorf("kubegen/util: error creating codec for %q – %v", contentType, err)
	}

	data, err := runtime.Encode(codec, object)
	if err != nil {
		return nil, fmt.Errorf("kubegen/util: error encoding object to %q – %v", contentType, err)
	}

	return cleanup(contentType, data)
}

func EncodeList(list *api.List, contentType string, pretty bool) ([]byte, error) {
	codec, err := makeCodec(contentType, pretty)
	if err != nil {
		return nil, fmt.Errorf("kubegen/util: error creating codec for %q – %v", contentType, err)
	}
	// XXX: uncommenting this results in the following error:
	// json: error calling MarshalJSON for type runtime.RawExtension: invalid character 'a' looking for beginning of value
	//if err := runtime.EncodeList(codec, list.Items); err != nil {
	//	return nil, err
	//}

	data, err := runtime.Encode(codec, list)
	if err != nil {
		return nil, fmt.Errorf("kubegen/util: error encoding list to %q – %v", contentType, err)
	}

	return cleanup(contentType, data)
}

func DumpListToFiles(list *api.List, contentType string) ([]string, error) {
	filenames := []string{}
	for _, item := range list.Items {
		var (
			name, filename, filenamefmt string
		)

		switch item.GetObjectKind().GroupVersionKind().Kind {
		case "Service":
			filenamefmt = "%s-svc.%s"
			name = item.(*v1.Service).ObjectMeta.Name
		case "Deployment":
			filenamefmt = "%s-dpl.%s"
			name = item.(*extensionsv1beta1.Deployment).ObjectMeta.Name
		case "ReplicaSet":
			filenamefmt = "%s-rs.%s"
			name = item.(*extensionsv1beta1.ReplicaSet).ObjectMeta.Name
		case "DaemonSet":
			filenamefmt = "%s-ds.%s"
			name = item.(*extensionsv1beta1.DaemonSet).ObjectMeta.Name
		case "StatefulSet":
			filenamefmt = "%s-ss.%s"
			name = item.(*appsv1beta1.StatefulSet).ObjectMeta.Name
		}

		data, err := Encode(item, contentType, true)
		if err != nil {
			return nil, err
		}

		switch contentType {
		case "application/yaml":
			filename = fmt.Sprintf(filenamefmt, name, "yaml")
			data = append([]byte(fmt.Sprintf("# generated by kubegen\n# => %s\n---\n", filename)), data...)
		case "application/json":
			filename = fmt.Sprintf(filenamefmt, name, "yaml")
		}

		if err := ioutil.WriteFile(filename, data, 0644); err != nil {
			return nil, fmt.Errorf("kubegen/util: error writing to file %q – %v", filename, err)
		}
		filenames = append(filenames, filename)
	}

	return filenames, nil
}

func NewFromHCL(obj interface{}, data []byte) error {
	manifest, err := hcl.Parse(string(data))
	if err != nil {
		return fmt.Errorf("kubegen/util: error parsing HCL – %v", err)
	}

	if err := hcl.DecodeObject(obj, manifest); err != nil {
		return fmt.Errorf("kubegen/util: error constructing an object from HCL – %v", err)
	}

	return nil
}
