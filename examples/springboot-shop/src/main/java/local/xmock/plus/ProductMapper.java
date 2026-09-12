package local.xmock.plus;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Mapper;
import org.springframework.context.annotation.Profile;

@Mapper
@Profile("mybatis-plus")
public interface ProductMapper extends BaseMapper<PlusProduct> {}
