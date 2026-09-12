package local.xmock;
import com.zaxxer.hikari.HikariConfig;
import com.zaxxer.hikari.HikariDataSource;
import java.sql.*;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class DriverContractTest {
 static HikariDataSource pool() {
  String url = System.getenv("X_MOCK_MYSQL_URL");
  assertNotNull(url,"integration harness must supply X_MOCK_MYSQL_URL");
  HikariConfig c = new HikariConfig();c.setJdbcUrl(url);c.setUsername("mock");c.setPassword("mock-local");c.setMaximumPoolSize(2);c.setMinimumIdle(0);c.setConnectionTimeout(3000);c.setInitializationFailTimeout(1);
  return new HikariDataSource(c);
 }
 @Test void metadataAndLargeIntegers() throws Exception {
  System.out.println("Runtime Java " + Runtime.version());
  try(var pool=pool();var c=pool.getConnection();var q=c.prepareStatement("SELECT id, status FROM orders WHERE id = ?")){
   assertTrue(c.isValid(2));assertTrue(c.getAutoCommit());assertEquals(Connection.TRANSACTION_READ_COMMITTED,c.getTransactionIsolation());
   if(System.getenv("X_MOCK_MYSQL_URL").contains("useServerPrepStmts=true")){assertEquals(Types.BIGINT,q.getMetaData().getColumnType(1));assertEquals("status",q.getMetaData().getColumnLabel(2));}
   q.setLong(1,9007199254740993L);try(var r=q.executeQuery()){assertTrue(r.next());assertEquals(9007199254740993L,r.getLong("id"));assertNull(r.getString("status"));assertTrue(r.wasNull());assertFalse(r.next());}
   q.setLong(1,1001);try(var r=q.executeQuery()){assertTrue(r.next());assertEquals("PAID",r.getString("status"));assertFalse(r.next());}
   q.setLong(1,404);try(var r=q.executeQuery()){assertFalse(r.next());assertEquals(Types.BIGINT,r.getMetaData().getColumnType(1));assertEquals("status",r.getMetaData().getColumnLabel(2));}
  }
  // Hikari evicts HY000 connections; each negative query needs a live session.
  for(String sql:new String[]{"SELECT id, status FROM orders WHERE id > 1001","SELECT id FROM missing WHERE id = 1001","CREATE TABLE forbidden (id BIGINT)"}){
   try(var pool=pool();var c=pool.getConnection();var s=c.createStatement()){
    SQLException error=assertThrows(SQLException.class,()->s.execute(sql));
    assertTrue(error.getErrorCode()==1105||error.getErrorCode()==1235);
   }
  }
 }
}
