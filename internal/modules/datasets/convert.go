package datasets

import (
	"fmt"
	"strings"

	"github.com/kwilteam/kwil-db/core/types/transactions"
	engineTypes "github.com/kwilteam/kwil-db/internal/engine/types"
)

func ConvertSchemaToEngine(old *transactions.Schema) (*engineTypes.Schema, error) {
	procedures, err := convertActionsToEngine(old.Actions)
	if err != nil {
		return nil, err
	}

	tables, err := convertTablesToEngine(old.Tables)
	if err != nil {
		return nil, err
	}

	extensions, err := convertExtensionsToEngine(old.Extensions)
	if err != nil {
		return nil, err
	}

	return &engineTypes.Schema{
		Name:       old.Name,
		Tables:     tables,
		Procedures: procedures,
		Extensions: extensions,
	}, nil
}

func convertTablesToEngine(tables []*transactions.Table) ([]*engineTypes.Table, error) {
	convTables := make([]*engineTypes.Table, len(tables))
	for i, table := range tables {
		columns, err := convertColumnsToEngine(table.Columns)
		if err != nil {
			return nil, err
		}

		indexes, err := convertIndexesToEngine(table.Indexes)
		if err != nil {
			return nil, err
		}

		foreignKeys, err := convertForeignKeysToEngine(table.ForeignKeys)
		if err != nil {
			return nil, err
		}

		convTables[i] = &engineTypes.Table{
			Name:        table.Name,
			Columns:     columns,
			Indexes:     indexes,
			ForeignKeys: foreignKeys,
		}
	}

	return convTables, nil
}

func convertColumnsToEngine(columns []*transactions.Column) ([]*engineTypes.Column, error) {
	convColumns := make([]*engineTypes.Column, len(columns))
	for i, column := range columns {
		colType := engineTypes.DataType(column.Type)
		if err := colType.Clean(); err != nil {
			return nil, err
		}

		attributes, err := convertAttributesToEngine(column.Attributes)
		if err != nil {
			return nil, err
		}

		convColumns[i] = &engineTypes.Column{
			Name:       column.Name,
			Type:       colType,
			Attributes: attributes,
		}
	}

	return convColumns, nil
}

func convertAttributesToEngine(attributes []*transactions.Attribute) ([]*engineTypes.Attribute, error) {
	convAttributes := make([]*engineTypes.Attribute, len(attributes))
	for i, attribute := range attributes {
		attrType := engineTypes.AttributeType(attribute.Type)
		if err := attrType.Clean(); err != nil {
			return nil, err
		}

		convAttributes[i] = &engineTypes.Attribute{
			Type:  attrType,
			Value: attribute.Value, // Assuming you have a function to parse the value based on its type
		}
	}

	return convAttributes, nil
}

func convertIndexesToEngine(indexes []*transactions.Index) ([]*engineTypes.Index, error) {
	convIndexes := make([]*engineTypes.Index, len(indexes))
	for i, index := range indexes {
		indexType := engineTypes.IndexType(index.Type)
		if err := indexType.Clean(); err != nil {
			return nil, err
		}

		convIndexes[i] = &engineTypes.Index{
			Name:    index.Name,
			Columns: index.Columns,
			Type:    indexType,
		}
	}

	return convIndexes, nil
}

func convertActionsToEngine(actions []*transactions.Action) ([]*engineTypes.Procedure, error) {
	convActions := make([]*engineTypes.Procedure, len(actions))
	for i, action := range actions {
		mods, err := convertModifiersToEngine(action.Mutability, action.Auxiliaries)
		if err != nil {
			return nil, err
		}

		convActions[i] = &engineTypes.Procedure{
			Name:        action.Name,
			Annotations: action.Annotations,
			Public:      action.Public,
			Modifiers:   mods,
			Args:        action.Inputs,
			Statements:  action.Statements,
		}
	}

	return convActions, nil
}

func convertModifiersToEngine(mutability string, auxiliaries []string) ([]engineTypes.Modifier, error) {
	mods := make([]engineTypes.Modifier, 0)
	switch strings.ToLower(mutability) {
	case transactions.MutabilityUpdate.String():
		break
	case transactions.MutabilityView.String():
		mods = append(mods, engineTypes.ModifierView)
	default:
		return nil, fmt.Errorf("unknown mutability type: %v", mutability)
	}

	for _, aux := range auxiliaries {
		switch strings.ToLower(aux) {
		case transactions.AuxiliaryTypeMustSign.String():
			mods = append(mods, engineTypes.ModifierAuthenticated)
		case transactions.AuxiliaryTypeOwner.String():
			mods = append(mods, engineTypes.ModifierOwner)
		default:
			return nil, fmt.Errorf("unknown auxiliary type: %v", aux)
		}
	}

	return mods, nil
}

func convertExtensionsToEngine(extensions []*transactions.Extension) ([]*engineTypes.Extension, error) {
	convExtensions := make([]*engineTypes.Extension, len(extensions))
	for i, extension := range extensions {
		convExtensions[i] = &engineTypes.Extension{
			Name:           extension.Name,
			Initialization: convertExtensionConfigToEngine(extension.Config),
			Alias:          extension.Alias,
		}
	}

	return convExtensions, nil
}

func convertExtensionConfigToEngine(configs []*transactions.ExtensionConfig) map[string]string {
	conf := make(map[string]string)
	for _, param := range configs {
		conf[param.Argument] = param.Value
	}

	return conf
}

func convertForeignKeysToEngine(fks []*transactions.ForeignKey) ([]*engineTypes.ForeignKey, error) {
	results := make([]*engineTypes.ForeignKey, len(fks))
	for i, fk := range fks {
		actions := make([]*engineTypes.ForeignKeyAction, len(fk.Actions))
		for j, action := range fk.Actions {
			newAction := &engineTypes.ForeignKeyAction{
				On: engineTypes.ForeignKeyActionOn(action.On),
				Do: engineTypes.ForeignKeyActionDo(action.Do),
			}
			err := newAction.Clean()
			if err != nil {
				return nil, err
			}

			actions[j] = newAction
		}

		results[i] = &engineTypes.ForeignKey{
			ChildKeys:   fk.ChildKeys,
			ParentKeys:  fk.ParentKeys,
			ParentTable: fk.ParentTable,
			Actions:     actions,
		}
	}

	return results, nil
}
