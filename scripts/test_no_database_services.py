import unittest
from check_no_database_services import violations


class DatabaseBoundaryTest(unittest.TestCase):
    def test_allows_client_and_offline_parser(self):
        self.assertEqual([], violations(
            'github.com/pingcap/tidb/pkg/parser/ast\ngithub.com/go-mysql-org/go-mysql/server',
            '[INFO] +- com.mysql:mysql-connector-j:jar:9.7.0:runtime'))

    def test_rejects_real_or_substitute_engines_and_container_bootstrap(self):
        for go, java in [
            ('github.com/pingcap/tidb/pkg/store/tikv', ''),
            ('github.com/dolthub/go-mysql-server/sql', ''),
            ('modernc.org/sqlite', ''),
            ('github.com/testcontainers/testcontainers-go', ''),
            ('', '[INFO] +- com.h2database:h2:jar:2.4.240:test'),
            ('', '[INFO] +- org.testcontainers:mysql:jar:1.21.0:test'),
            ('', '[INFO] +- com.wix:wix-embedded-mysql:jar:4.6.0:test'),
        ]:
            with self.subTest(go=go, java=java):
                self.assertTrue(violations(go, java), 'database service/engine dependency was accepted')


if __name__ == '__main__':
    unittest.main()
