package tfclient

import (
	"encoding/json"
	"fmt"

	"github.com/adrien-f/tf-data-client/internal/tfplugin6"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	"github.com/zclconf/go-cty/cty/msgpack"
)

// schemaBlockToType converts a proto schema block to a cty.Type
func schemaBlockToType(block *tfplugin6.Schema_Block) (cty.Type, error) {
	if block == nil {
		return cty.EmptyObject, nil
	}

	attrTypes := make(map[string]cty.Type)

	// Process attributes
	for _, attr := range block.Attributes {
		var attrType cty.Type

		if attr.NestedType != nil {
			// Nested object type
			nestedType, err := nestedObjectToType(attr.NestedType)
			if err != nil {
				return cty.NilType, fmt.Errorf("failed to convert nested type for %s: %w", attr.Name, err)
			}
			attrType = nestedType
		} else if len(attr.Type) > 0 {
			// JSON-encoded cty type
			if err := json.Unmarshal(attr.Type, &attrType); err != nil {
				return cty.NilType, fmt.Errorf("failed to unmarshal type for %s: %w", attr.Name, err)
			}
		} else {
			attrType = cty.DynamicPseudoType
		}

		attrTypes[attr.Name] = attrType
	}

	// Process nested blocks
	for _, blockType := range block.BlockTypes {
		nestedType, err := schemaBlockToType(blockType.Block)
		if err != nil {
			return cty.NilType, fmt.Errorf("failed to convert nested block %s: %w", blockType.TypeName, err)
		}

		switch blockType.Nesting {
		case tfplugin6.Schema_NestedBlock_SINGLE, tfplugin6.Schema_NestedBlock_GROUP:
			attrTypes[blockType.TypeName] = nestedType
		case tfplugin6.Schema_NestedBlock_LIST:
			attrTypes[blockType.TypeName] = cty.List(nestedType)
		case tfplugin6.Schema_NestedBlock_SET:
			attrTypes[blockType.TypeName] = cty.Set(nestedType)
		case tfplugin6.Schema_NestedBlock_MAP:
			attrTypes[blockType.TypeName] = cty.Map(nestedType)
		}
	}

	return cty.Object(attrTypes), nil
}

// nestedObjectToType converts a nested object schema to a cty.Type
func nestedObjectToType(obj *tfplugin6.Schema_Object) (cty.Type, error) {
	attrTypes := make(map[string]cty.Type)

	for _, attr := range obj.Attributes {
		var attrType cty.Type

		if attr.NestedType != nil {
			nestedType, err := nestedObjectToType(attr.NestedType)
			if err != nil {
				return cty.NilType, fmt.Errorf("failed to convert nested type for %s: %w", attr.Name, err)
			}
			attrType = nestedType
		} else if len(attr.Type) > 0 {
			if err := json.Unmarshal(attr.Type, &attrType); err != nil {
				return cty.NilType, fmt.Errorf("failed to unmarshal type for %s: %w", attr.Name, err)
			}
		} else {
			attrType = cty.DynamicPseudoType
		}

		attrTypes[attr.Name] = attrType
	}

	objType := cty.Object(attrTypes)

	switch obj.Nesting {
	case tfplugin6.Schema_Object_SINGLE:
		return objType, nil
	case tfplugin6.Schema_Object_LIST:
		return cty.List(objType), nil
	case tfplugin6.Schema_Object_SET:
		return cty.Set(objType), nil
	case tfplugin6.Schema_Object_MAP:
		return cty.Map(objType), nil
	default:
		return objType, nil
	}
}

// mapToCtyValue converts a Go map to a cty.Value using the given type
func mapToCtyValue(m map[string]interface{}, ty cty.Type) (cty.Value, error) {
	if m == nil {
		return cty.NullVal(ty), nil
	}

	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to marshal map to JSON: %w", err)
	}

	val, err := ctyjson.Unmarshal(jsonBytes, ty)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to unmarshal JSON to cty value: %w", err)
	}

	return val, nil
}

// ctyValueToMap converts a cty.Value to a Go map
func ctyValueToMap(val cty.Value) (map[string]interface{}, error) {
	if val.IsNull() {
		return nil, nil
	}

	jsonBytes, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cty value to JSON: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON to map: %w", err)
	}

	return result, nil
}

// decodeDynamicValue decodes a DynamicValue proto message to a cty.Value
func decodeDynamicValue(dv *tfplugin6.DynamicValue, ty cty.Type) (cty.Value, error) {
	if dv == nil {
		return cty.NullVal(ty), nil
	}

	if len(dv.Msgpack) > 0 {
		return msgpack.Unmarshal(dv.Msgpack, ty)
	}

	if len(dv.Json) > 0 {
		return ctyjson.Unmarshal(dv.Json, ty)
	}

	return cty.NullVal(ty), nil
}
