package local.xmock;
import java.sql.*;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class TransactionContractTest {
 static final String READ="SELECT id, quantity FROM cart_items WHERE user_id = ? AND product_id = ?";
 static final String INSERT="INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)";
 static final String UPDATE="UPDATE cart_items SET quantity = quantity + ? WHERE user_id = ? AND product_id = ?";
 static final String NOOP="UPDATE cart_items SET quantity = ? WHERE user_id = ? AND product_id = ?";
 static long quantity(Connection c,long user)throws SQLException{
  try(var q=c.prepareStatement(READ)){q.setLong(1,user);q.setLong(2,1001);try(var r=q.executeQuery()){if(!r.next())return 0;long qty=r.getLong("quantity");assertFalse(r.next());return qty;}}
 }
 static long insert(Connection c,long user)throws SQLException{
  try(var q=c.prepareStatement(INSERT,Statement.RETURN_GENERATED_KEYS)){q.setLong(1,user);q.setLong(2,1001);q.setLong(3,1);assertEquals(1,q.executeUpdate());try(var r=q.getGeneratedKeys()){assertTrue(r.next());return r.getLong(1);}}
 }
 static int update(Connection c,String sql,long amount)throws SQLException{
  try(var q=c.prepareStatement(sql)){q.setLong(1,amount);q.setLong(2,2001);q.setLong(3,1001);return q.executeUpdate();}
 }
 @Test void visibilityRollbackConflictAndPoolReuse()throws Exception{
  try(var pool=DriverContractTest.pool();var a=pool.getConnection();var b=pool.getConnection()){
   assertEquals(0,quantity(a,2001));a.setAutoCommit(false);assertEquals(5001,insert(a,2001));assertEquals(1,quantity(a,2001));assertEquals(0,quantity(b,2001));a.rollback();assertEquals(0,quantity(b,2001));
   assertEquals(5002,insert(a,2001));a.commit();assertEquals(1,quantity(b,2001));assertEquals(0,quantity(b,2002));
   a.setAutoCommit(true);assertEquals(1,update(a,UPDATE,1));assertEquals(2,quantity(b,2001));
   boolean changed=System.getenv("X_MOCK_MYSQL_URL").contains("useAffectedRows=true");assertEquals(changed?0:1,update(a,NOOP,2));
   a.setAutoCommit(false);b.setAutoCommit(false);assertEquals(1,update(a,UPDATE,1));assertEquals(1,update(b,UPDATE,1));a.commit();SQLException conflict=assertThrows(SQLException.class,b::commit);assertEquals("40001",conflict.getSQLState());b.rollback();assertEquals(3,quantity(b,2001));
   assertThrows(SQLException.class,()->{try(var s=a.createStatement()){s.execute("SAVEPOINT unsupported");}});
   assertThrows(SQLException.class,()->a.setTransactionIsolation(Connection.TRANSACTION_REPEATABLE_READ));
   a.rollback();a.setAutoCommit(true);b.rollback();b.setAutoCommit(true);
  }
  // Returning a connection with pending work must roll it back.
  try(var pool=DriverContractTest.pool()){
   try(var c=pool.getConnection()){c.setAutoCommit(false);insert(c,2002);assertEquals(1,quantity(c,2002));}
   try(var c=pool.getConnection()){assertTrue(c.getAutoCommit());assertEquals(0,quantity(c,2002));}
  }
 }
}
