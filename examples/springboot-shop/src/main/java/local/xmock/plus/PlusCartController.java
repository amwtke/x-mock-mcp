package local.xmock.plus;

import jakarta.servlet.http.HttpServletRequest;
import local.xmock.TestIdentity;
import org.springframework.context.annotation.Profile;
import org.springframework.http.ResponseEntity;
import org.springframework.transaction.annotation.Isolation;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.bind.annotation.*;

@RestController
@Profile("mybatis-plus")
public class PlusCartController {
  private final PlusShopRepository repository;
  private final TestIdentity identity;
  public PlusCartController(PlusShopRepository repository, TestIdentity identity) {
    this.repository = repository;
    this.identity = identity;
  }
  @DeleteMapping("/api/cart/items/{id}")
  @Transactional(isolation=Isolation.READ_COMMITTED)
  public ResponseEntity<Void> remove(@PathVariable long id, HttpServletRequest request) {
    int changed = repository.removeForUser(id, identity.user(request));
    return changed == 1 ? ResponseEntity.noContent().build() : ResponseEntity.notFound().build();
  }
}
