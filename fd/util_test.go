package fd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/bitly/go-simplejson"
	"github.com/ozontech/file.d/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_extractConditions(t *testing.T) {
	j, err := simplejson.NewJson([]byte(`{"service": ["address-api", "tarifficator-api", "card-api", "teams-api"]}`))
	require.NoError(t, err)
	got, err := extractConditions(j)
	require.NoError(t, err)
	expected := pipeline.MatchConditions{
		pipeline.MatchCondition{
			Field:  []string{"service"},
			Values: []string{"address-api", "tarifficator-api", "card-api", "teams-api"},
		},
	}
	require.Equal(t, expected, got)

	j, err = simplejson.NewJson([]byte(`{"service": "address-api"}`))
	require.NoError(t, err)
	got, err = extractConditions(j)
	require.NoError(t, err)
	expected = pipeline.MatchConditions{
		pipeline.MatchCondition{
			Field:  []string{"service"},
			Values: []string{"address-api"},
		},
	}
	require.Equal(t, expected, got)
}

type doIfTreeNode struct {
	fieldOp       string
	fieldName     string
	caseSensitive bool
	values        [][]byte

	logicalOp string
	operands  []*doIfTreeNode

	byteLenCmpOp string
	cmpValue     int
}

// nolint:gocritic
func buildDoIfTree(node *doIfTreeNode) (pipeline.DoIfNode, error) {
	switch {
	case node.fieldOp != "":
		return pipeline.NewFieldOpNode(
			node.fieldOp,
			node.fieldName,
			node.caseSensitive,
			node.values,
		)
	case node.logicalOp != "":
		operands := make([]pipeline.DoIfNode, 0)
		for _, operandNode := range node.operands {
			operand, err := buildDoIfTree(operandNode)
			if err != nil {
				return nil, fmt.Errorf("failed to build tree: %w", err)
			}
			operands = append(operands, operand)
		}
		return pipeline.NewLogicalNode(
			node.logicalOp,
			operands,
		)
	case node.byteLenCmpOp != "":
		return pipeline.NewByteLengthCmpNode(node.fieldName, node.byteLenCmpOp, node.cmpValue)
	default:
		return nil, errors.New("unknown type of node")
	}
}

func Test_extractDoIfChecker(t *testing.T) {
	type args struct {
		cfgStr string
	}

	tests := []struct {
		name    string
		args    args
		want    *doIfTreeNode
		wantErr bool
	}{
		{
			name: "ok",
			args: args{
				cfgStr: `
		{
			"op": "not",
			"operands": [
				{
					"op": "and",
					"operands": [
						{
							"op": "equal",
							"field": "service",
							"values": [null, ""],
							"case_sensitive": false
						},
						{
							"op": "prefix",
							"field": "log.msg",
							"values": ["test-1", "test-2"],
							"case_sensitive": false
						},
						{
							"op": "byte_len_cmp",
							"field": "msg",
							"cmp_op": "gt",
							"value": 100
						},
						{
							"op": "or",
							"operands": [
								{
									"op": "suffix",
									"field": "service",
									"values": ["test-svc-1", "test-svc-2"],
									"case_sensitive": true
								},
								{
									"op": "contains",
									"field": "pod",
									"values": ["test"]
								},
								{
									"op": "regex",
									"field": "message",
									"values": ["test-\\d+", "test-msg-\\d+"]
								}
							]
						}
					]
				}
			]
		}
						`,
			},
			want: &doIfTreeNode{
				logicalOp: "not",
				operands: []*doIfTreeNode{
					{
						logicalOp: "and",
						operands: []*doIfTreeNode{
							{
								fieldOp:       "equal",
								fieldName:     "service",
								values:        [][]byte{nil, []byte("")},
								caseSensitive: false,
							},
							{
								fieldOp:       "prefix",
								fieldName:     "log.msg",
								values:        [][]byte{[]byte("test-1"), []byte("test-2")},
								caseSensitive: false,
							},
							{
								byteLenCmpOp: "gt",
								fieldName:    "msg",
								cmpValue:     100,
							},
							{
								logicalOp: "or",
								operands: []*doIfTreeNode{
									{
										fieldOp:       "suffix",
										fieldName:     "service",
										values:        [][]byte{[]byte("test-svc-1"), []byte("test-svc-2")},
										caseSensitive: true,
									},
									{
										fieldOp:       "contains",
										fieldName:     "pod",
										values:        [][]byte{[]byte("test")},
										caseSensitive: true,
									},
									{
										fieldOp:       "regex",
										fieldName:     "message",
										values:        [][]byte{[]byte(`test-\d+`), []byte(`test-msg-\d+`)},
										caseSensitive: true,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "ok_not_map",
			args: args{
				cfgStr: `[{"field":"val"}]`,
			},
			wantErr: false,
		},
		{
			name: "ok_byte_len_cmp_op",
			args: args{
				cfgStr: `{"op":"byte_len_cmp","field":"data","cmp_op":"lt","value":10}`,
			},
			want: &doIfTreeNode{
				byteLenCmpOp: "lt",
				fieldName:    "data",
				cmpValue:     10,
			},
		},
		{
			name: "ok_single_val",
			args: args{
				cfgStr: `{
					"op":"or",
					"operands":[
						{"op":"equal","field":"service","values":null},
						{"op":"equal","field":"service","values":""},
						{"op":"equal","field":"service","values":"test"}
					]
				}`,
			},
			want: &doIfTreeNode{
				logicalOp: "or",
				operands: []*doIfTreeNode{
					{
						fieldOp:       "equal",
						fieldName:     "service",
						values:        [][]byte{nil},
						caseSensitive: true,
					},
					{
						fieldOp:       "equal",
						fieldName:     "service",
						values:        [][]byte{[]byte("")},
						caseSensitive: true,
					},
					{
						fieldOp:       "equal",
						fieldName:     "service",
						values:        [][]byte{[]byte("test")},
						caseSensitive: true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "error_no_op_field",
			args: args{
				cfgStr: `{"field": "val"}`,
			},
			wantErr: true,
		},
		{
			name: "error_invalid_op_name",
			args: args{
				cfgStr: `{"op": "invalid"}`,
			},
			wantErr: true,
		},
		{
			name: "error_invalid_field_op",
			args: args{
				cfgStr: `{"op": "equal"}`,
			},
			wantErr: true,
		},
		{
			name: "error_invalid_logical_op",
			args: args{
				cfgStr: `{"op": "or"}`,
			},
			wantErr: true,
		},
		{
			name: "error_invalid_logical_op_operand",
			args: args{
				cfgStr: `{"op": "or", "operands": [{"op": "equal"}]}`,
			},
			wantErr: true,
		},
		{
			name: "error_byte_len_cmp_op_no_field",
			args: args{
				cfgStr: `{"op":"byte_len_cmp","cmp_op":"lt","value":10}`,
			},
			wantErr: true,
		},
		{
			name: "error_byte_len_cmp_op_field_is_not_string",
			args: args{
				cfgStr: `{"op":"byte_len_cmp","field":123,"cmp_op":"lt","value":10}`,
			},
			wantErr: true,
		},
		{
			name: "error_byte_len_cmp_op_no_cmp_op",
			args: args{
				cfgStr: `{"op":"byte_len_cmp","field":"data","value":10}`,
			},
			wantErr: true,
		},
		{
			name: "error_byte_len_cmp_op_cmp_op_is_not_string",
			args: args{
				cfgStr: `{"op":"byte_len_cmp","field":"data","cmp_op":123,"value":10}`,
			},
			wantErr: true,
		},
		{
			name: "error_byte_len_cmp_op_no_cmp_value",
			args: args{
				cfgStr: `{"op":"byte_len_cmp","field":"data","cmp_op":"lt"}`,
			},
			wantErr: true,
		},
		{
			name: "error_byte_len_cmp_op_cmp_value_is_not_integer",
			args: args{
				cfgStr: `{"op":"byte_len_cmp","field":"data","cmp_op":"lt","value":"abc"}`,
			},
			wantErr: true,
		},
		{
			name:    "error_byte_len_cmp_op_invalid_cmp_op",
			args:    args{cfgStr: `{"op":"byte_len_cmp","field":"data","cmp_op":"ABC","value":10}`},
			wantErr: true,
		},
		{
			name:    "error_byte_len_cmp_op_negative_cmp_value",
			args:    args{cfgStr: `{"op":"byte_len_cmp","field":"data","cmp_op":"lt","value":-1}`},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader := bytes.NewBufferString(tt.args.cfgStr)
			actionJSON, err := simplejson.NewFromReader(reader)
			require.NoError(t, err)
			got, err := extractDoIfChecker(actionJSON)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractDoIfChecker() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			wantTree, err := buildDoIfTree(tt.want)
			require.NoError(t, err)
			wantDoIfChecker := pipeline.NewDoIfChecker(wantTree)
			assert.NoError(t, wantDoIfChecker.IsEqualTo(got))
		})
	}
}
