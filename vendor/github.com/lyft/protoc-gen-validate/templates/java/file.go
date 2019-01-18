package java

const fileTpl = `// Code generated by protoc-gen-validate. DO NOT EDIT.
// source: {{ .InputPath }}

package {{ javaPackage . }};

public class {{ classNameFile . }}Validator {
	public static com.lyft.pgv.Validator validatorFor(Class clazz) {
		{{ range .AllMessages }}
		if (clazz.equals({{ qualifiedName . }}.class)) return new {{ simpleName .}}Validator();
		{{- end }}
		return null;
	}

{{ range .AllMessages -}}
	{{- template "msg" . -}}
{{- end }}
}
`
