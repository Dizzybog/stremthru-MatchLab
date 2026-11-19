package torrent_info

import (
	"fmt"
	"strings"

	"github.com/MunifTanjim/stremthru/internal/db"
)

// postgresResetColumns replaces only derived columns, rather than updating
// every torrent row. Postgres 11+ stores non-volatile ADD COLUMN defaults in
// table metadata without rewriting the heap. The old physical values remain
// unavailable to queries until a later normal rewrite/VACUUM FULL.
//
// DROP COLUMN automatically removes dependent indexes. Snapshot all affected
// index definitions, including user-created indexes and expression/predicate
// dependencies, and restore them before the reset transaction commits.
//
// Hard-coded schema/table names and column declarations are intentional: never
// interpolate externally controlled identifiers into DDL.
func postgresResetColumns(tx *db.Tx, table string, columnNames []string, alterSQL string) error {
	if table != "public.torrent_info" && table != "public.torrent_stream" {
		return fmt.Errorf("unsupported MatchLab reset table: %s", table)
	}
	if len(columnNames) == 0 {
		return fmt.Errorf("no columns specified for %s", table)
	}
	for _, name := range columnNames {
		switch name {
		case "sid", "asid", "matchlab_mapped_at", "matchlab_retry_at",
			"matchlab_parse_retry_at", "matchlab_file_revision",
			"parser_version", "parser_input", "parsed_at":
		default:
			return fmt.Errorf("unsupported MatchLab reset column: %s", name)
		}
	}
	quoted := make([]string, 0, len(columnNames))
	for _, name := range columnNames {
		quoted = append(quoted, "'"+name+"'")
	}

	// pg_depend catches plain index keys, INCLUDE columns, expression indexes
	// and partial-index predicates. Never drop an index-backed constraint:
	// restoring a bare index would silently lose the original constraint.
	rows, err := tx.Query(`
		SELECT DISTINCT pg_get_indexdef(i.indexrelid),
		       COALESCE(c.contype::text, '')
		FROM pg_index AS i
		JOIN pg_depend AS d
		  ON d.classid = 'pg_class'::regclass
		 AND d.objid = i.indexrelid
		 AND d.refobjid = i.indrelid
		JOIN pg_attribute AS a
		  ON a.attrelid = i.indrelid
		 AND a.attnum = d.refobjsubid
		LEFT JOIN pg_constraint AS c ON c.conindid = i.indexrelid
		WHERE i.indrelid = ?::regclass
		  AND a.attname IN (`+strings.Join(quoted, ", ")+`)
		ORDER BY 1`, table)
	if err != nil {
		return fmt.Errorf("inspect %s indexes: %w", table, err)
	}

	indexDefinitions := []string{}
	for rows.Next() {
		var definition, constraintType string
		if err := rows.Scan(&definition, &constraintType); err != nil {
			rows.Close()
			return fmt.Errorf("read %s index definition: %w", table, err)
		}
		if constraintType != "" {
			rows.Close()
			return fmt.Errorf("%s contains an indexed constraint depending on reset columns; refusing destructive DDL", table)
		}
		indexDefinitions = append(indexDefinitions, definition)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read %s indexes: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s index scan: %w", table, err)
	}

	if _, err := tx.Exec(alterSQL); err != nil {
		return fmt.Errorf("reset %s columns: %w", table, err)
	}
	for _, definition := range indexDefinitions {
		if _, err := tx.Exec(definition); err != nil {
			return fmt.Errorf("restore %s index (%s): %w", table, definition, err)
		}
	}
	return nil
}

func postgresResetMatchLabMetadata(tx *db.Tx, includeParsing bool) error {
	// The new revision must differ from every pre-reset revision, so in-flight
	// matches cannot commit old SID/ASID data after the maintenance gate opens.
	var nextRevision int64
	if err := tx.QueryRow(
		"SELECT COALESCE(MAX(matchlab_file_revision), 0) + 1 FROM public.torrent_info",
	).Scan(&nextRevision); err != nil {
		return fmt.Errorf("find next MatchLab reset revision: %w", err)
	}

	infoColumns := []string{"matchlab_mapped_at", "matchlab_retry_at", "matchlab_parse_retry_at", "matchlab_file_revision"}
	infoDDL := `ALTER TABLE public.torrent_info
		DROP COLUMN matchlab_mapped_at,
		DROP COLUMN matchlab_retry_at,
		DROP COLUMN matchlab_parse_retry_at,
		DROP COLUMN matchlab_file_revision,
		ADD COLUMN matchlab_mapped_at timestamptz,
		ADD COLUMN matchlab_retry_at timestamptz,
		ADD COLUMN matchlab_parse_retry_at timestamptz,
		ADD COLUMN matchlab_file_revision bigint NOT NULL DEFAULT `+fmt.Sprint(nextRevision)
	if includeParsing {
		infoColumns = append(infoColumns, "parser_version", "parser_input", "parsed_at")
		infoDDL += `,
		DROP COLUMN parser_version,
		DROP COLUMN parser_input,
		DROP COLUMN parsed_at,
		ADD COLUMN parser_version integer NOT NULL DEFAULT 0,
		ADD COLUMN parser_input text NOT NULL DEFAULT '',
		ADD COLUMN parsed_at timestamptz`
	}
	if err := postgresResetColumns(tx, "public.torrent_info", infoColumns, infoDDL); err != nil {
		return err
	}
	// New hashes normally start at revision zero; existing hashes have a
	// strictly higher epoch after this global reset.
	if _, err := tx.Exec(
		"ALTER TABLE public.torrent_info ALTER COLUMN matchlab_file_revision SET DEFAULT 0",
	); err != nil {
		return fmt.Errorf("restore torrent_info file revision default: %w", err)
	}
	return nil
}

func postgresResetMatchLabStreamIDs(tx *db.Tx) error {
	// sid was originally varchar NOT NULL DEFAULT ''. Existing rows should
	// become '*' (unmatched); restore the original default for new rows.
	if err := postgresResetColumns(tx, "public.torrent_stream", []string{"sid", "asid"}, `
		ALTER TABLE public.torrent_stream
		DROP COLUMN sid,
		DROP COLUMN asid,
		ADD COLUMN sid character varying NOT NULL DEFAULT '*',
		ADD COLUMN asid text NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := tx.Exec("ALTER TABLE public.torrent_stream ALTER COLUMN sid SET DEFAULT ''"); err != nil {
		return fmt.Errorf("restore torrent_stream sid default: %w", err)
	}
	return nil
}
